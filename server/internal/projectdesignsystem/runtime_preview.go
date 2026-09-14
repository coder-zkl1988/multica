package projectdesignsystem

import (
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
)

// RuntimePreviewBridgeScript returns the only script allowed to execute in a
// project-design-system preview. The optional capability binds selection events
// from the user-facing preview route; the daemon verification route leaves it
// empty because it has no parent UI consumer.
func RuntimePreviewBridgeScript(capability string) string {
	capability = strings.TrimSpace(capability)
	capabilityField := ""
	if capability != "" {
		capabilityField = ",capability:" + strconv.Quote(capability)
	}
	expectedCapability := strconv.Quote(capability)
	return "(()=>{let enabled=false;const expectedCapability=" + expectedCapability + ";window.addEventListener(\"message\",event=>{if(event.source!==parent)return;const message=event.data;if(!message||message.type!==\"multica:project-design-system-selection-mode\")return;if(expectedCapability&&message.capability!==expectedCapability)return;enabled=message.enabled===true});document.addEventListener(\"click\",event=>{if(!enabled)return;const target=event.target;const node=target instanceof Element?target.closest(\"[data-design-node-id]\"):null;if(!node)return;event.preventDefault();event.stopPropagation();parent.postMessage({type:\"multica:project-design-system-select\",id:node.dataset.designNodeId" + capabilityField + "},\"*\")},true)})();"
}

// RuntimePreviewCSP is shared by the daemon's real-browser gate and the final
// user-facing preview route. Inline styles are allowed because the package audit
// already parses and constrains every preview stylesheet; scripts remain pinned
// to the exact trusted bridge hash.
func RuntimePreviewCSP(bridgeScript string) string {
	digest := sha256.Sum256([]byte(bridgeScript))
	return RuntimePreviewCSPFromHash(base64.StdEncoding.EncodeToString(digest[:]))
}

// RuntimePreviewCSPFromHash keeps callers that already cache the bridge hash on
// the same directive set as callers that hold the script bytes.
func RuntimePreviewCSPFromHash(bridgeScriptHash string) string {
	return "default-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'sha256-" +
		bridgeScriptHash +
		"'; connect-src 'none'; object-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'"
}

// InjectRuntimePreviewHTML injects the shared Token source and trusted bridge
// into a validated preview target. Every admitted target lives exactly one
// directory below the package root, so ../tokens.css resolves correctly in both
// the loopback verifier and the authenticated user-facing file route.
func InjectRuntimePreviewHTML(raw []byte, bridgeScript string) []byte {
	body := string(raw)
	const linkTag = `<link rel="stylesheet" href="../tokens.css">`
	scriptTag := "<script>" + bridgeScript + "</script>"
	if index := strings.Index(body, "</head>"); index >= 0 {
		body = body[:index] + linkTag + body[index:]
	} else {
		body = linkTag + body
	}
	if index := strings.Index(body, "</body>"); index >= 0 {
		body = body[:index] + scriptTag + body[index:]
	} else {
		body += scriptTag
	}
	return []byte(body)
}
