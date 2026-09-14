package designdocument

import (
	"net/url"
	"path"
	"strings"

	parse "github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"
)

// The design document prototype deliberately allows package-local JavaScript
// and localStorage so page switching, tabs, filters, sorting, overlays, form
// validation and mock data transitions can be demonstrated for real. What it
// must never do is need the network.
//
// The audit parses every script and walks the grammar-aware AST. It rejects
// forbidden free globals and remote URL strings, applies contextual member
// rules, and rejects constructs that would make static analysis meaningless:
// eval, the Function constructor, dynamic import, and computed lookups on a
// global object.
//
// Static analysis cannot decide every dynamically composed expression. That is
// why the constructs which enable dynamic composition are rejected outright,
// and why the browser Preview gate still runs the prototype with the network
// unavailable and reports any outbound request. This audit is the static half
// of that pair, never a replacement for it.

// forbiddenScriptGlobals are identifiers a prototype has no reason to mention.
// Every one of them is either a way to reach the network, a way to install
// background code, or a way to build code at runtime.
var forbiddenScriptGlobals = map[string]string{
	"fetch":             "network requests are not allowed in a prototype",
	"XMLHttpRequest":    "network requests are not allowed in a prototype",
	"WebSocket":         "WebSocket connections are not allowed in a prototype",
	"EventSource":       "server sent events are not allowed in a prototype",
	"sendBeacon":        "beacon requests are not allowed in a prototype",
	"serviceWorker":     "Service Worker registration is not allowed in a prototype",
	"importScripts":     "remote script loading is not allowed in a prototype",
	"SharedWorker":      "shared workers are not allowed in a prototype",
	"RTCPeerConnection": "peer connections are not allowed in a prototype",
	"WebTransport":      "transport connections are not allowed in a prototype",
	"eval":              "runtime code evaluation is not allowed in a prototype",
}

// globalAliases are the identifiers that reach the host window or document.
// Navigation members are only forbidden when they hang off one of these, so a
// mock data field such as row.location stays perfectly legal.
var globalAliases = map[string]struct{}{
	"window": {}, "self": {}, "globalThis": {}, "document": {}, "navigator": {},
	"top": {}, "parent": {}, "frames": {},
}

// navigationMembers navigate the Preview away from the package, open a new
// browsing context, or rewrite the document from a string.
var navigationMembers = map[string]struct{}{
	"location": {}, "open": {}, "opener": {}, "write": {}, "writeln": {},
	"top": {}, "parent": {}, "frames": {},
}

// freeNavigationIdentifiers are the bare globals that navigate or reach out of
// the prototype frame without needing a window prefix.
var freeNavigationIdentifiers = map[string]struct{}{
	"location": {}, "opener": {}, "top": {}, "parent": {}, "frames": {},
}

// constructedGlobals may not be constructed. Worker and Function are ordinary
// words that a prototype could legitimately use as a data field name, so they
// are rejected only in `new` position where the meaning is unambiguous.
var constructedGlobals = map[string]string{
	"Worker":   "web workers are not allowed in a prototype",
	"Function": "runtime code construction is not allowed in a prototype",
}

// activeElementTags may not be created from script. Creating one of these is
// the standard way to pull remote code or a remote document into the page.
var activeElementTags = map[string]struct{}{
	"script": {}, "link": {}, "iframe": {}, "embed": {}, "object": {},
	"frame": {}, "frameset": {}, "base": {}, "meta": {}, "portal": {},
}

// forbiddenURLSchemes are the schemes that mean network access or code
// execution. Other schemes, including data: images, are left alone because a
// prototype has no API left with which to fetch them.
var forbiddenURLSchemes = map[string]struct{}{
	"http": {}, "https": {}, "ws": {}, "wss": {}, "ftp": {}, "ftps": {},
	"file": {}, "javascript": {}, "vbscript": {}, "blob": {},
}

type scriptAudit struct {
	path                string
	artifacts           map[string]ArtifactIndexEntry
	globalBindings      map[*js.Var]struct{}
	functions           map[*js.Var][]*js.Params
	safeDOMQueryFuncs   map[*js.Var]safeDOMQueryFunction
	safeDOMQueryReturns map[js.IExpr]struct{}
	diagnostics         []Diagnostic
}

type globalBindingCollector struct {
	bindings          map[*js.Var]struct{}
	functions         map[*js.Var][]*js.Params
	safeDOMQueryFuncs map[*js.Var]safeDOMQueryFunction
	changed           bool
}

type declaredFunctionCollector struct {
	functions map[*js.Var][]*js.Params
	changed   bool
}

type globalAliasFinder struct {
	bindings                map[*js.Var]struct{}
	functions               map[*js.Var][]*js.Params
	safeDOMQueryFuncs       map[*js.Var]safeDOMQueryFunction
	skipKnownPrimitiveCalls bool
	found                   bool
}

type safeDOMQueryFunctionCollector struct {
	functions map[*js.Var]safeDOMQueryFunction
	returns   map[js.IExpr]struct{}
}

type safeDOMQueryFunction struct {
	rootArgumentIndex int
}

// auditScript parses one prototype script and applies the AST audit.
func auditScript(source []byte, basePath string, artifacts map[string]ArtifactIndexEntry) []Diagnostic {
	ast, err := js.Parse(parse.NewInputBytes(source), js.Options{})
	if err != nil {
		return []Diagnostic{errorDiagnostic("prototype_script_invalid", basePath, "prototype JavaScript is invalid: "+err.Error())}
	}
	functions := make(map[*js.Var][]*js.Params)
	for {
		collector := &declaredFunctionCollector{functions: functions}
		js.Walk(collector, ast)
		if !collector.changed {
			break
		}
	}
	safeDOMQueryFuncs := make(map[*js.Var]safeDOMQueryFunction)
	safeDOMQueryReturns := make(map[js.IExpr]struct{})
	js.Walk(&safeDOMQueryFunctionCollector{functions: safeDOMQueryFuncs, returns: safeDOMQueryReturns}, ast)
	globalBindings := make(map[*js.Var]struct{})
	// Resolve alias chains to a fixed point before policy checks so source order
	// and function placement cannot hide a global-derived binding.
	for {
		collector := &globalBindingCollector{bindings: globalBindings, functions: functions, safeDOMQueryFuncs: safeDOMQueryFuncs}
		js.Walk(collector, ast)
		if !collector.changed {
			break
		}
	}
	audit := &scriptAudit{
		path:                basePath,
		artifacts:           artifacts,
		globalBindings:      globalBindings,
		functions:           functions,
		safeDOMQueryFuncs:   safeDOMQueryFuncs,
		safeDOMQueryReturns: safeDOMQueryReturns,
		diagnostics:         make([]Diagnostic, 0),
	}
	js.Walk(audit, ast)
	return audit.diagnostics
}

func (audit *scriptAudit) Enter(node js.INode) js.IVisitor {
	switch value := node.(type) {
	case *js.BindingElement:
		if value.Default != nil && expressionContainsGlobalAlias(value.Default, audit.globalBindings, audit.safeDOMQueryFuncs) {
			audit.checkGlobalBinding(value.Binding)
		}
	case *js.Var:
		name, declaration := resolveVar(value)
		if declaration != js.NoDecl {
			return nil
		}
		if message, forbidden := forbiddenScriptGlobals[name]; forbidden {
			audit.report("prototype_script_forbidden_api", message)
		}
		if _, forbidden := freeNavigationIdentifiers[name]; forbidden {
			audit.report("prototype_script_navigation_forbidden", "prototype scripts cannot reference "+name)
		}
		return nil
	case *js.LiteralExpr:
		if value.TokenType == js.StringToken {
			audit.checkURLString(decodeJSString(value.Data))
		}
		return nil
	case *js.TemplateExpr:
		for _, part := range value.List {
			audit.checkURLString(decodeJSString(trimTemplateDelimiters(part.Value)))
		}
		audit.checkURLString(decodeJSString(trimTemplateDelimiters(value.Tail)))
	case *js.DirectivePrologueStmt:
		audit.checkURLString(decodeJSString(value.Value))
		return nil
	case *js.DotExpr:
		if name, ok := memberName(value.Y); ok {
			audit.checkMember(value.X, name)
		}
		js.Walk(audit, value.X)
		return nil
	case *js.IndexExpr:
		if name, ok := stringLiteralValue(value.Y); ok {
			audit.checkMember(value.X, name)
			audit.checkComputedName(name)
		} else if audit.isGlobalAlias(value.X) {
			audit.report("prototype_script_dynamic_global", "computed lookups on a global object are not allowed")
		}
		js.Walk(audit, value.X)
		js.Walk(audit, value.Y)
		return nil
	case *js.CallExpr:
		audit.checkCall(value.X, value.Args)
	case *js.ReturnStmt:
		if value.Value != nil && !audit.isSafeDOMQueryReturn(value.Value) && expressionReturnContainsGlobalAlias(value.Value, audit.globalBindings, audit.functions, audit.safeDOMQueryFuncs) &&
			!definitelyPrimitiveResult(value.Value) {
			audit.report("prototype_script_dynamic_global", "returning a global capability from a prototype function is not allowed")
		}
	case *js.BinaryExpr:
		if isGlobalAssignmentToken(value.Op) {
			switch value.X.(type) {
			case *js.DotExpr, *js.IndexExpr:
				if expressionContainsGlobalAlias(value.Y, audit.globalBindings, audit.safeDOMQueryFuncs) && !definitelyPrimitiveResult(value.Y) {
					audit.checkGlobalAssignment(value.X)
				}
			default:
				if expressionContainsGlobalAlias(value.Y, audit.globalBindings, audit.safeDOMQueryFuncs) {
					audit.checkGlobalAssignment(value.X)
				}
			}
		}
	case *js.NewExpr:
		if name, ok := freeIdentifierName(value.X); ok {
			if message, forbidden := constructedGlobals[name]; forbidden {
				audit.report("prototype_script_forbidden_api", message)
			}
		}
	case *js.ImportStmt:
		module := decodeJSString(value.Module)
		audit.checkURLString(module)
		audit.checkModuleSpecifier(module)
	case *js.ExportStmt:
		if value.Module != nil {
			audit.checkURLString(decodeJSString(value.Module))
		}
	}
	return audit
}

func (audit *scriptAudit) Exit(js.INode) {}

func (collector *globalBindingCollector) Enter(node js.INode) js.IVisitor {
	switch value := node.(type) {
	case *js.BindingElement:
		if value.Default != nil && expressionContainsGlobalAlias(value.Default, collector.bindings, collector.safeDOMQueryFuncs) &&
			!definitelyPrimitiveResult(value.Default) {
			collector.collectBinding(value.Binding)
		}
	case *js.BinaryExpr:
		if isGlobalAssignmentToken(value.Op) && expressionContainsGlobalAlias(value.Y, collector.bindings, collector.safeDOMQueryFuncs) &&
			!definitelyPrimitiveResult(value.Y) {
			collector.collectAssignment(value.X)
		}
	case *js.CallExpr:
		for _, params := range declaredFunctionParams(value.X, collector.functions) {
			for index, argument := range value.Args.List {
				if expressionContainsGlobalAlias(argument.Value, collector.bindings, collector.safeDOMQueryFuncs) && !definitelyPrimitiveResult(argument.Value) {
					collector.collectBinding(parameterBinding(params, index))
				}
			}
		}
	}
	return collector
}

func (collector *globalBindingCollector) Exit(js.INode) {}

func (collector *declaredFunctionCollector) Enter(node js.INode) js.IVisitor {
	switch value := node.(type) {
	case *js.FuncDecl:
		if value.Name != nil {
			collector.add(value.Name, &value.Params)
		}
	case *js.BindingElement:
		variable, ok := value.Binding.(*js.Var)
		if ok && value.Default != nil {
			collector.addAll(variable, declaredFunctionParams(value.Default, collector.functions))
		}
	case *js.BinaryExpr:
		variable, ok := value.X.(*js.Var)
		if ok && value.Op == js.EqToken {
			collector.addAll(variable, declaredFunctionParams(value.Y, collector.functions))
		}
	}
	return collector
}

func (collector *declaredFunctionCollector) Exit(js.INode) {}

func (collector *safeDOMQueryFunctionCollector) Enter(node js.INode) js.IVisitor {
	value, ok := node.(*js.BindingElement)
	if !ok || value.Default == nil {
		return collector
	}
	variable, ok := value.Binding.(*js.Var)
	if !ok {
		return collector
	}
	returned, rootArgumentIndex, ok := safeDOMQueryFunctionReturn(value.Default)
	if !ok {
		return collector
	}
	collector.functions[resolveVarRoot(variable)] = safeDOMQueryFunction{rootArgumentIndex: rootArgumentIndex}
	collector.returns[returned] = struct{}{}
	return collector
}

func (collector *safeDOMQueryFunctionCollector) Exit(js.INode) {}

func (collector *declaredFunctionCollector) addAll(variable *js.Var, candidates []*js.Params) {
	for _, params := range candidates {
		collector.add(variable, params)
	}
}

func (collector *declaredFunctionCollector) add(variable *js.Var, params *js.Params) {
	if params == nil {
		return
	}
	root := resolveVarRoot(variable)
	for _, existing := range collector.functions[root] {
		if existing == params {
			return
		}
	}
	collector.functions[root] = append(collector.functions[root], params)
	collector.changed = true
}

func (finder *globalAliasFinder) Enter(node js.INode) js.IVisitor {
	if finder.found {
		return nil
	}
	if call, ok := node.(*js.CallExpr); ok && isSafeDOMQueryFunctionCall(call, finder.bindings, finder.safeDOMQueryFuncs) {
		return nil
	}
	if call, ok := node.(*js.CallExpr); ok && finder.skipKnownPrimitiveCalls {
		if len(declaredFunctionParams(call.X, finder.functions)) != 0 {
			finder.found = expressionContainsGlobalAlias(call.X, finder.bindings, finder.safeDOMQueryFuncs)
			return nil
		}
		if knownPrimitiveReturnCall(call) {
			return nil
		}
	}
	if expr, ok := node.(js.IExpr); ok && expressionIsGlobalAlias(expr, finder.bindings) {
		finder.found = true
		return nil
	}
	return finder
}

func (finder *globalAliasFinder) Exit(js.INode) {}

func (collector *globalBindingCollector) collectBinding(binding js.IBinding) {
	switch value := binding.(type) {
	case *js.Var:
		collector.add(value)
	case *js.BindingArray:
		for _, item := range value.List {
			collector.collectBinding(item.Binding)
		}
		collector.collectBinding(value.Rest)
	case *js.BindingObject:
		for _, item := range value.List {
			collector.collectBinding(item.Value.Binding)
		}
		if value.Rest != nil {
			collector.add(value.Rest)
		}
	}
}

func (collector *globalBindingCollector) collectAssignment(expr js.IExpr) {
	switch value := expr.(type) {
	case *js.Var:
		collector.add(value)
	case *js.GroupExpr:
		collector.collectAssignment(value.X)
	case *js.ArrayExpr:
		for _, item := range value.List {
			collector.collectAssignment(item.Value)
		}
	case *js.ObjectExpr:
		for _, item := range value.List {
			collector.collectAssignment(item.Value)
		}
	case *js.BinaryExpr:
		if value.Op == js.EqToken {
			collector.collectAssignment(value.X)
		}
	}
}

func (collector *globalBindingCollector) add(variable *js.Var) {
	root := resolveVarRoot(variable)
	if _, exists := collector.bindings[root]; exists {
		return
	}
	collector.bindings[root] = struct{}{}
	collector.changed = true
}

func (audit *scriptAudit) checkCall(callee js.IExpr, args js.Args) {
	for _, params := range declaredFunctionParams(callee, audit.functions) {
		for index, argument := range args.List {
			if expressionContainsGlobalAlias(argument.Value, audit.globalBindings, audit.safeDOMQueryFuncs) && !definitelyPrimitiveResult(argument.Value) {
				audit.checkGlobalBinding(parameterBinding(params, index))
			}
		}
	}
	if literal, ok := callee.(*js.LiteralExpr); ok && literal.TokenType == js.ImportToken {
		audit.report("prototype_script_dynamic_import", "dynamic import is not allowed in a prototype")
		return
	}
	if name, ok := freeIdentifierName(callee); ok {
		if message, forbidden := constructedGlobals[name]; forbidden && name == "Function" {
			audit.report("prototype_script_forbidden_api", message)
		}
	}
	dot, ok := callee.(*js.DotExpr)
	if !ok {
		return
	}
	property, ok := memberName(dot.Y)
	if !ok || (property != "createElement" && property != "createElementNS") {
		return
	}
	for _, argument := range args.List {
		tag, ok := stringLiteralValue(argument.Value)
		if !ok {
			continue
		}
		if _, forbidden := activeElementTags[strings.ToLower(strings.TrimSpace(tag))]; forbidden {
			audit.report("prototype_script_active_element", "creating <"+tag+"> from script is not allowed")
		}
	}
}

func (audit *scriptAudit) checkMember(object js.IExpr, property string) {
	if message, forbidden := forbiddenScriptGlobals[property]; forbidden {
		audit.report("prototype_script_forbidden_api", message)
		return
	}
	if !audit.isGlobalAlias(object) {
		return
	}
	if _, forbidden := navigationMembers[property]; forbidden {
		audit.report("prototype_script_navigation_forbidden", "prototype scripts cannot use this navigation member: "+property)
	}
}

func (audit *scriptAudit) checkGlobalBinding(binding js.IBinding) {
	switch value := binding.(type) {
	case *js.Var:
		audit.globalBindings[resolveVarRoot(value)] = struct{}{}
	case *js.BindingArray:
		for _, item := range value.List {
			audit.checkGlobalBinding(item.Binding)
		}
		if value.Rest != nil {
			audit.checkGlobalBinding(value.Rest)
		}
	case *js.BindingObject:
		for _, item := range value.List {
			name, ok := bindingPropertyName(item.Key)
			if !ok {
				audit.report("prototype_script_dynamic_global", "computed destructuring on a global object is not allowed")
				continue
			}
			audit.checkExtractedMember(name)
			audit.checkGlobalBinding(item.Value.Binding)
		}
		if value.Rest != nil {
			audit.checkGlobalBinding(value.Rest)
		}
	}
}

func (audit *scriptAudit) checkGlobalAssignment(expr js.IExpr) {
	switch value := expr.(type) {
	case *js.Var:
		audit.globalBindings[resolveVarRoot(value)] = struct{}{}
	case *js.GroupExpr:
		audit.checkGlobalAssignment(value.X)
	case *js.DotExpr, *js.IndexExpr:
		audit.report("prototype_script_dynamic_global", "assigning a global object through a property is not allowed")
	case *js.ArrayExpr:
		for _, item := range value.List {
			audit.checkGlobalAssignment(item.Value)
		}
	case *js.ObjectExpr:
		for _, item := range value.List {
			if item.Name != nil {
				name, ok := bindingPropertyName(item.Name)
				if !ok {
					audit.report("prototype_script_dynamic_global", "computed destructuring on a global object is not allowed")
					continue
				}
				audit.checkExtractedMember(name)
			}
			audit.checkGlobalAssignment(item.Value)
		}
	case *js.BinaryExpr:
		if value.Op == js.EqToken {
			audit.checkGlobalAssignment(value.X)
		}
	}
}

func (audit *scriptAudit) checkExtractedMember(property string) {
	if message, forbidden := forbiddenScriptGlobals[property]; forbidden {
		audit.report("prototype_script_forbidden_api", message)
		return
	}
	if message, forbidden := constructedGlobals[property]; forbidden {
		audit.report("prototype_script_forbidden_api", message)
		return
	}
	if _, forbidden := navigationMembers[property]; forbidden {
		audit.report("prototype_script_navigation_forbidden", "prototype scripts cannot extract this navigation member: "+property)
	}
}

func (audit *scriptAudit) isGlobalAlias(expr js.IExpr) bool {
	return expressionIsGlobalAlias(expr, audit.globalBindings)
}

func expressionContainsGlobalAlias(expr js.IExpr, bindings map[*js.Var]struct{}, safeDOMQueryFuncs map[*js.Var]safeDOMQueryFunction) bool {
	finder := &globalAliasFinder{bindings: bindings, safeDOMQueryFuncs: safeDOMQueryFuncs}
	js.Walk(finder, expr)
	return finder.found
}

func expressionReturnContainsGlobalAlias(
	expr js.IExpr,
	bindings map[*js.Var]struct{},
	functions map[*js.Var][]*js.Params,
	safeDOMQueryFuncs map[*js.Var]safeDOMQueryFunction,
) bool {
	finder := &globalAliasFinder{bindings: bindings, functions: functions, safeDOMQueryFuncs: safeDOMQueryFuncs, skipKnownPrimitiveCalls: true}
	js.Walk(finder, expr)
	return finder.found
}

func isGlobalAssignmentToken(token js.TokenType) bool {
	return token == js.EqToken || token == js.AndEqToken || token == js.OrEqToken || token == js.NullishEqToken
}

func declaredFunctionParams(callee js.IExpr, functions map[*js.Var][]*js.Params) []*js.Params {
	switch value := callee.(type) {
	case *js.GroupExpr:
		return declaredFunctionParams(value.X, functions)
	case *js.ArrowFunc:
		return []*js.Params{&value.Params}
	case *js.FuncDecl:
		return []*js.Params{&value.Params}
	}
	variable, ok := callee.(*js.Var)
	if !ok {
		return nil
	}
	return functions[resolveVarRoot(variable)]
}

func knownPrimitiveReturnCall(call *js.CallExpr) bool {
	callee, ok := call.X.(*js.DotExpr)
	if !ok {
		return false
	}
	member, ok := memberName(callee.Y)
	if !ok {
		return false
	}
	if member != "getItem" {
		return false
	}
	storage, ok := callee.X.(*js.DotExpr)
	if !ok {
		return false
	}
	name, named := memberName(storage.Y)
	root, rooted := freeIdentifierName(storage.X)
	return named && name == "localStorage" && rooted && root == "window"
}

func parameterBinding(params *js.Params, index int) js.IBinding {
	if index < len(params.List) {
		return params.List[index].Binding
	}
	return params.Rest
}

func (audit *scriptAudit) isSafeDOMQueryReturn(expr js.IExpr) bool {
	_, ok := audit.safeDOMQueryReturns[expr]
	return ok
}

func safeDOMQueryFunctionReturn(expr js.IExpr) (js.IExpr, int, bool) {
	function, ok := expr.(*js.ArrowFunc)
	if !ok || function.Async || len(function.Params.List) != 2 || len(function.Body.List) != 1 {
		return nil, 0, false
	}
	returned, ok := function.Body.List[0].(*js.ReturnStmt)
	if !ok || returned.Value == nil {
		return nil, 0, false
	}
	selector, ok := function.Params.List[0].Binding.(*js.Var)
	if !ok || function.Params.List[0].Default != nil {
		return nil, 0, false
	}
	root, ok := function.Params.List[1].Binding.(*js.Var)
	if !ok || function.Params.List[1].Default == nil {
		return nil, 0, false
	}
	name, isDocument := freeIdentifierName(function.Params.List[1].Default)
	if !isDocument || name != "document" || !safeDOMQueryResult(returned.Value, resolveVarRoot(selector), resolveVarRoot(root)) {
		return nil, 0, false
	}
	return returned.Value, 1, true
}

func isSafeDOMQueryFunctionCall(call *js.CallExpr, bindings map[*js.Var]struct{}, functions map[*js.Var]safeDOMQueryFunction) bool {
	variable, ok := call.X.(*js.Var)
	if !ok {
		return false
	}
	function, ok := functions[resolveVarRoot(variable)]
	if !ok {
		return false
	}
	if function.rootArgumentIndex >= len(call.Args.List) {
		return true
	}
	if expressionContainsGlobalAlias(call.Args.List[function.rootArgumentIndex].Value, bindings, nil) {
		return false
	}
	return true
}

func safeDOMQueryResult(expr js.IExpr, selector, root *js.Var) bool {
	if call, ok := expr.(*js.CallExpr); ok {
		return safeDOMQueryCall(call, selector, root)
	}
	array, ok := expr.(*js.ArrayExpr)
	if !ok || len(array.List) != 1 || !array.List[0].Spread {
		return false
	}
	call, ok := array.List[0].Value.(*js.CallExpr)
	return ok && safeDOMQueryCall(call, selector, root)
}

func safeDOMQueryCall(call *js.CallExpr, selector, root *js.Var) bool {
	member, ok := call.X.(*js.DotExpr)
	if !ok {
		return false
	}
	name, ok := memberName(member.Y)
	if !ok || (name != "querySelector" && name != "querySelectorAll") {
		return false
	}
	variable, ok := member.X.(*js.Var)
	if !ok || resolveVarRoot(variable) != root || len(call.Args.List) != 1 {
		return false
	}
	argument, ok := call.Args.List[0].Value.(*js.Var)
	return ok && resolveVarRoot(argument) == selector
}

func definitelyPrimitiveResult(expr js.IExpr) bool {
	switch value := expr.(type) {
	case *js.UnaryExpr:
		return value.Op == js.NotToken
	case *js.BinaryExpr:
		return value.Op == js.EqEqEqToken || value.Op == js.NotEqEqToken
	case *js.CallExpr:
		return knownPrimitiveDOMValueCall(value)
	default:
		return false
	}
}

func knownPrimitiveDOMValueCall(call *js.CallExpr) bool {
	trim, ok := call.X.(*js.DotExpr)
	if !ok {
		return false
	}
	name, named := memberName(trim.Y)
	if !named || name != "trim" {
		return false
	}
	value, ok := trim.X.(*js.DotExpr)
	if !ok {
		return false
	}
	name, named = memberName(value.Y)
	if !named || name != "value" {
		return false
	}
	lookup, ok := value.X.(*js.CallExpr)
	if !ok {
		return false
	}
	getElement, ok := lookup.X.(*js.DotExpr)
	if !ok {
		return false
	}
	name, named = memberName(getElement.Y)
	root, rooted := freeIdentifierName(getElement.X)
	return named && name == "getElementById" && rooted && root == "document"
}

func expressionIsGlobalAlias(expr js.IExpr, bindings map[*js.Var]struct{}) bool {
	switch value := expr.(type) {
	case *js.GroupExpr:
		return expressionIsGlobalAlias(value.X, bindings)
	case *js.DotExpr:
		name, ok := memberName(value.Y)
		if !ok {
			return false
		}
		_, alias := globalAliases[name]
		return alias && expressionIsGlobalAlias(value.X, bindings)
	case *js.IndexExpr:
		name, ok := stringLiteralValue(value.Y)
		if !ok {
			return false
		}
		_, alias := globalAliases[name]
		return alias && expressionIsGlobalAlias(value.X, bindings)
	}
	variable, ok := expr.(*js.Var)
	if !ok {
		return false
	}
	root := resolveVarRoot(variable)
	name, declaration := resolveVar(root)
	if declaration == js.NoDecl {
		_, alias := globalAliases[name]
		return alias
	}
	_, alias := bindings[root]
	return alias
}

func (audit *scriptAudit) checkComputedName(name string) {
	if message, forbidden := forbiddenScriptGlobals[name]; forbidden {
		audit.report("prototype_script_forbidden_api", message)
		return
	}
	if _, forbidden := constructedGlobals[name]; forbidden {
		audit.report("prototype_script_forbidden_api", "runtime lookup of "+name+" is not allowed")
	}
}

// checkModuleSpecifier requires an ES module import to stay inside the package.
func (audit *scriptAudit) checkModuleSpecifier(specifier string) {
	resolved, ok := resolveLocalResource(specifier, audit.path)
	if !ok || strings.HasPrefix(resolved, "#") {
		audit.report("prototype_script_module_external", "module imports must resolve inside the package")
		return
	}
	entry, exists := audit.artifacts[resolved]
	if !exists || entry.Role != "prototype_script" {
		audit.report("prototype_script_module_external", "module imports must resolve to a package-local prototype script")
	}
}

func (audit *scriptAudit) checkURLString(value string) {
	if scriptStringIsRemoteURL(value) {
		audit.report("prototype_script_remote_url", "prototype scripts cannot contain remote or executable URLs")
	}
}

func (audit *scriptAudit) report(code, message string) {
	audit.diagnostics = append(audit.diagnostics, errorDiagnostic(code, audit.path, message))
}

// scriptStringIsRemoteURL reports whether a decoded string is an absolute
// remote URL or a protocol-relative one.
func scriptStringIsRemoteURL(value string) bool {
	normalized := normalizeForURLPolicy(value)
	if normalized == "" {
		return false
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return false
	}
	if strings.HasPrefix(normalized, "//") {
		return parsed.Host != ""
	}
	_, forbidden := forbiddenURLSchemes[strings.ToLower(parsed.Scheme)]
	return forbidden
}

func resolveVar(variable *js.Var) (string, js.DeclType) {
	variable = resolveVarRoot(variable)
	return string(variable.Data), variable.Decl
}

func resolveVarRoot(variable *js.Var) *js.Var {
	for variable.Link != nil {
		variable = variable.Link
	}
	return variable
}

func freeIdentifierName(expr js.IExpr) (string, bool) {
	variable, ok := expr.(*js.Var)
	if !ok {
		return "", false
	}
	name, declaration := resolveVar(variable)
	if declaration != js.NoDecl {
		return "", false
	}
	return name, true
}

// memberName reads the property name of a dot expression. The parser stores it
// as a literal value, or as a variable for a private class field.
func memberName(expr js.IExpr) (string, bool) {
	switch value := expr.(type) {
	case js.LiteralExpr:
		return string(value.Data), true
	case *js.LiteralExpr:
		return string(value.Data), true
	case *js.Var:
		name, _ := resolveVar(value)
		return name, true
	}
	return "", false
}

func bindingPropertyName(property *js.PropertyName) (string, bool) {
	if property == nil {
		return "", false
	}
	if property.Computed != nil {
		return stringLiteralValue(property.Computed)
	}
	if property.Literal.TokenType == js.StringToken {
		return decodeJSString(property.Literal.Data), true
	}
	return string(property.Literal.Data), true
}

func stringLiteralValue(expr js.IExpr) (string, bool) {
	literal, ok := expr.(*js.LiteralExpr)
	if !ok || literal.TokenType != js.StringToken {
		return "", false
	}
	return decodeJSString(literal.Data), true
}

func trimTemplateDelimiters(data []byte) []byte {
	value := data
	if len(value) > 0 && (value[0] == '`' || value[0] == '}') {
		value = value[1:]
	}
	if len(value) > 0 && value[len(value)-1] == '`' {
		value = value[:len(value)-1]
	} else if len(value) >= 2 && value[len(value)-2] == '$' && value[len(value)-1] == '{' {
		value = value[:len(value)-2]
	}
	return value
}

// decodeJSString unquotes and unescapes a JavaScript string literal so an
// escaped scheme cannot hide a remote URL from the policy check.
func decodeJSString(data []byte) string {
	value := string(data)
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') || (first == '`' && last == '`') {
			value = value[1 : len(value)-1]
		}
	}
	var decoded strings.Builder
	decoded.Grow(len(value))
	for index := 0; index < len(value); {
		if value[index] != '\\' {
			decoded.WriteByte(value[index])
			index++
			continue
		}
		index++
		if index == len(value) {
			break
		}
		switch value[index] {
		case 'n':
			decoded.WriteByte('\n')
			index++
		case 't':
			decoded.WriteByte('\t')
			index++
		case 'r':
			decoded.WriteByte('\r')
			index++
		case 'b', 'f', 'v', '0':
			decoded.WriteByte(' ')
			index++
		case '\n':
			index++
		case '\r':
			index++
			if index < len(value) && value[index] == '\n' {
				index++
			}
		case 'x':
			index++
			decoded.WriteRune(readHexCodePoint(value, &index, 2))
		case 'u':
			index++
			if index < len(value) && value[index] == '{' {
				index++
				start := index
				for index < len(value) && value[index] != '}' {
					index++
				}
				decoded.WriteRune(parseCodePoint(value[start:index]))
				if index < len(value) {
					index++
				}
				continue
			}
			decoded.WriteRune(readHexCodePoint(value, &index, 4))
		default:
			decoded.WriteByte(value[index])
			index++
		}
	}
	return decoded.String()
}

func readHexCodePoint(value string, index *int, width int) rune {
	start := *index
	end := start + width
	if end > len(value) {
		end = len(value)
	}
	*index = end
	return parseCodePoint(value[start:end])
}

func parseCodePoint(digits string) rune {
	if digits == "" {
		return '�'
	}
	var codePoint uint32
	for index := 0; index < len(digits); index++ {
		digit, ok := hexDigit(digits[index])
		if !ok {
			return '�'
		}
		codePoint = codePoint*16 + uint32(digit)
		if codePoint > 0x10ffff {
			return '�'
		}
	}
	if 0xd800 <= codePoint && codePoint <= 0xdfff {
		return '�'
	}
	return rune(codePoint)
}

// prototypeScriptPath reports whether a resolved package path is a prototype
// script, which is the only thing a <script src> or a module import may name.
func prototypeScriptPath(name string) bool {
	return strings.HasPrefix(name, prototypeRoot+"/") && strings.ToLower(path.Ext(name)) == ".js"
}
