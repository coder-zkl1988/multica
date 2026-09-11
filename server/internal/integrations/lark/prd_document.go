package lark

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// PRDDocumentClient is deliberately separate from the general IM APIClient.
// Callers must authorize the original human requester and serialize publication
// before invoking these methods. Credentials never enter agent-visible output.
type PRDDocumentClient interface {
	ResolvePRDTemplate(context.Context, InstallationCredentials, string) (string, error)
	CopyPRDTemplate(context.Context, InstallationCredentials, string, string) (PRDDocument, error)
	FillPRDDocument(context.Context, InstallationCredentials, string, []PRDSection) error
	FinalizePRDOwner(context.Context, InstallationCredentials, string, string) error
	VerifyPRDDocument(context.Context, InstallationCredentials, string, []PRDSection, string) error
	GetPRDDocumentURL(context.Context, InstallationCredentials, string) (string, error)
}

type PRDDocument struct {
	ID  string
	URL string
}

type PRDSection struct {
	Heading string `json:"heading"`
	Body    string `json:"body"`
}

var _ PRDDocumentClient = (*httpAPIClient)(nil)

// PRDCopyError distinguishes a rejected/unattempted copy from an uncertain
// outcome. Unknown outcomes must never trigger a new copy. A nonempty document
// ID returned alongside any error must be persisted and reused by the caller.
type PRDCopyError struct {
	OutcomeUnknown bool
	Err            error
}

func (e *PRDCopyError) Error() string {
	return fmt.Sprintf("lark PRD copy (outcome_unknown=%t): %v", e.OutcomeUnknown, e.Err)
}
func (e *PRDCopyError) Unwrap() error { return e.Err }

const (
	prdMaxResponseBytes = 4 << 20
	prdMaxBlocks        = 10000
	prdPendingText      = "【待确认】"
)

// prdJSON shares the existing HTTP transport, token cache and region resolver.
// Unlike the IM helper, document reads are bounded and redirects are forbidden.
// There are no blind retries, including for copy or owner transfer. A rejected
// token is evicted for the next explicitly resumed operation.
func (c *httpAPIClient) prdJSON(ctx context.Context, creds InstallationCredentials, method, path string, body, out any) error {
	ctx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()
	token, err := c.tenantAccessToken(ctx, creds)
	if err != nil {
		return err
	}
	var data []byte
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.resolveBaseURL(creds)+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	// Do not mark a non-idempotent request replayable to net/http transports.
	req.GetBody = nil
	client := *c.cfg.HTTPClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("lark PRD request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, prdMaxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("lark PRD response: %w", err)
	}
	if len(raw) > prdMaxResponseBytes {
		return errors.New("lark PRD response exceeds limit")
	}
	var envelope struct {
		Code *int            `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	decodeErr := json.Unmarshal(raw, &envelope)
	code := 0
	if envelope.Code != nil {
		code = *envelope.Code
	}
	if isTokenError(code) {
		c.invalidateToken(creds.AppID)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &larkAPIStatusError{StatusCode: resp.StatusCode, Code: code, Msg: envelope.Msg, Raw: fmt.Sprintf("code=%d", code)}
	}
	if decodeErr != nil || envelope.Code == nil {
		return errors.New("lark PRD invalid response envelope")
	}
	if code != 0 {
		return &APIError{Op: "PRD", Code: code, Msg: envelope.Msg}
	}
	if out != nil {
		if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
			return errors.New("lark PRD missing response data")
		}
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("lark PRD decode data: %w", err)
		}
	}
	return nil
}

func prdToken(token string) bool {
	if len(token) == 0 || len(token) > 256 {
		return false
	}
	for _, r := range token {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func (c *httpAPIClient) ResolvePRDTemplate(ctx context.Context, creds InstallationCredentials, wikiToken string) (string, error) {
	if !prdToken(wikiToken) {
		return "", errors.New("invalid PRD template wiki token")
	}
	var data struct {
		Node struct {
			Token       string `json:"node_token"`
			ObjectToken string `json:"obj_token"`
			ObjectType  string `json:"obj_type"`
		} `json:"node"`
	}
	if err := c.prdJSON(ctx, creds, http.MethodGet, "/open-apis/wiki/v2/spaces/get_node?obj_type=wiki&token="+url.QueryEscape(wikiToken), nil, &data); err != nil {
		return "", err
	}
	if data.Node.Token != wikiToken || data.Node.ObjectType != "docx" || !prdToken(data.Node.ObjectToken) {
		return "", errors.New("PRD template must resolve to the requested wiki node and a docx document")
	}
	return data.Node.ObjectToken, nil
}

type prdMetadata struct {
	Token string `json:"doc_token"`
	Type  string `json:"doc_type"`
	Owner string `json:"owner_id"`
	URL   string `json:"url"`
}

func (c *httpAPIClient) prdMetadata(ctx context.Context, creds InstallationCredentials, docID string) (prdMetadata, error) {
	if !prdToken(docID) {
		return prdMetadata{}, errors.New("invalid PRD document ID")
	}
	var data struct {
		Metas  []prdMetadata     `json:"metas"`
		Failed []json.RawMessage `json:"failed_list"`
	}
	body := map[string]any{"request_docs": []map[string]string{{"doc_token": docID, "doc_type": "docx"}}, "with_url": true}
	if err := c.prdJSON(ctx, creds, http.MethodPost, "/open-apis/drive/v1/metas/batch_query?user_id_type=open_id", body, &data); err != nil {
		return prdMetadata{}, err
	}
	if len(data.Failed) != 0 || len(data.Metas) != 1 || data.Metas[0].Token != docID || data.Metas[0].Type != "docx" || data.Metas[0].Owner == "" {
		return prdMetadata{}, errors.New("PRD docx metadata or owner unavailable")
	}
	return data.Metas[0], nil
}

// GetPRDDocumentURL recovers a usable link for a durably recorded copy ID.
// It never creates a document or fabricates a tenant URL.
func (c *httpAPIClient) GetPRDDocumentURL(ctx context.Context, creds InstallationCredentials, docID string) (string, error) {
	meta, err := c.prdMetadata(ctx, creds, docID)
	if err != nil {
		return "", err
	}
	if !prdDocumentURL(meta.URL, docID) {
		return "", errors.New("PRD metadata returned an invalid document URL")
	}
	return meta.URL, nil
}

func (c *httpAPIClient) CopyPRDTemplate(ctx context.Context, creds InstallationCredentials, sourceDocID, title string) (PRDDocument, error) {
	known := func(err error) (PRDDocument, error) { return PRDDocument{}, &PRDCopyError{Err: err} }
	if strings.TrimSpace(title) == "" || len(title) > 256 || !utf8.ValidString(title) {
		return known(errors.New("PRD title must contain 1-256 UTF-8 bytes"))
	}
	if _, err := c.prdMetadata(ctx, creds, sourceDocID); err != nil {
		return known(err)
	}
	var root struct {
		Token string `json:"token"`
	}
	if err := c.prdJSON(ctx, creds, http.MethodGet, "/open-apis/drive/explorer/v2/root_folder/meta", nil, &root); err != nil {
		return known(err)
	}
	if !prdToken(root.Token) {
		return known(errors.New("PRD destination root folder unavailable"))
	}
	var data struct {
		File struct {
			Token string `json:"token"`
			Type  string `json:"type"`
			URL   string `json:"url"`
		} `json:"file"`
	}
	err := c.prdJSON(ctx, creds, http.MethodPost, "/open-apis/drive/v1/files/"+sourceDocID+"/copy?user_id_type=open_id", map[string]string{"name": title, "type": "docx", "folder_token": root.Token}, &data)
	if err != nil {
		unknown := true
		var status *larkAPIStatusError
		if !errors.As(err, &status) || status.StatusCode < 500 {
			switch larkErrorCode(err) {
			case 1061002, 1061003, 1061004, 1061005, 1061007, 1062507, 1064510, 1064511, 99991663, 99991664, 99991672, 99991679, 99991400:
				unknown = false
			}
		}
		return PRDDocument{}, &PRDCopyError{OutcomeUnknown: unknown, Err: err}
	}
	doc := PRDDocument{ID: data.File.Token, URL: data.File.URL}
	if !prdToken(doc.ID) || doc.ID == sourceDocID {
		return PRDDocument{}, &PRDCopyError{OutcomeUnknown: true, Err: errors.New("copy response missing a new document ID")}
	}
	if data.File.Type != "docx" {
		doc.URL = ""
		return doc, &PRDCopyError{OutcomeUnknown: true, Err: errors.New("copy did not return a docx document")}
	}
	// URLs are never fetched; only token-addressed requests reach the configured
	// OpenAPI host. Still reject a malicious link in an upstream response.
	if !prdDocumentURL(doc.URL, doc.ID) {
		doc.URL = ""
		return doc, &PRDCopyError{OutcomeUnknown: true, Err: errors.New("copy returned an invalid document URL")}
	}
	return doc, nil
}

func prdDocumentURL(raw, docID string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Path != "/docx/"+docID {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "feishu.cn" || strings.HasSuffix(host, ".feishu.cn") || host == "larksuite.com" || strings.HasSuffix(host, ".larksuite.com")
}

type prdText struct {
	Elements []struct {
		TextRun *struct {
			Content string `json:"content"`
		} `json:"text_run"`
	} `json:"elements"`
}

func (t *prdText) plain() (string, bool) {
	if t == nil {
		return "", false
	}
	var b strings.Builder
	for _, e := range t.Elements {
		if e.TextRun == nil {
			return "", false
		}
		b.WriteString(e.TextRun.Content)
	}
	return b.String(), true
}

type prdBlock struct {
	ID       string   `json:"block_id"`
	Parent   string   `json:"parent_id"`
	Children []string `json:"children"`
	Type     int      `json:"block_type"`
	Text     *prdText `json:"text"`
	Heading1 *prdText `json:"heading1"`
	Heading2 *prdText `json:"heading2"`
	Heading3 *prdText `json:"heading3"`
	Heading4 *prdText `json:"heading4"`
	Heading5 *prdText `json:"heading5"`
	Heading6 *prdText `json:"heading6"`
	Heading7 *prdText `json:"heading7"`
	Heading8 *prdText `json:"heading8"`
	Heading9 *prdText `json:"heading9"`
}

func (b prdBlock) heading() (string, bool) {
	if b.Type < 3 || b.Type > 11 {
		return "", false
	}
	headings := [...]*prdText{b.Heading1, b.Heading2, b.Heading3, b.Heading4, b.Heading5, b.Heading6, b.Heading7, b.Heading8, b.Heading9}
	return headings[b.Type-3].plain()
}

type prdSnapshot struct {
	Revision int64
	Blocks   map[string]prdBlock
}

func (c *httpAPIClient) prdSnapshot(ctx context.Context, creds InstallationCredentials, docID string) (prdSnapshot, error) {
	snapshot := prdSnapshot{Blocks: make(map[string]prdBlock)}
	if !prdToken(docID) {
		return snapshot, errors.New("invalid PRD document ID")
	}
	var info struct {
		Document struct {
			ID       string `json:"document_id"`
			Revision int64  `json:"revision_id"`
		} `json:"document"`
	}
	path := "/open-apis/docx/v1/documents/" + docID
	if err := c.prdJSON(ctx, creds, http.MethodGet, path, nil, &info); err != nil {
		return snapshot, err
	}
	if info.Document.ID != docID || info.Document.Revision < 1 {
		return snapshot, errors.New("invalid PRD document revision")
	}
	snapshot.Revision = info.Document.Revision
	for attempt := range 2 {
		blockRevision := strconv.FormatInt(snapshot.Revision, 10)
		if attempt == 1 {
			// Some Feishu tenants allow the latest blocks but reject an explicit
			// revision for an otherwise readable document. Retry latest only for
			// that documented permission response, then verify the revision did
			// not change while reading so the snapshot remains immutable.
			blockRevision = "-1"
		}
		snapshot.Blocks = make(map[string]prdBlock)
		seen := map[string]bool{}
		page := ""
		pinnedPermissionFailure := false
		for {
			var data struct {
				Items     []prdBlock `json:"items"`
				HasMore   bool       `json:"has_more"`
				PageToken string     `json:"page_token"`
			}
			query := url.Values{"page_size": {"500"}, "document_revision_id": {blockRevision}, "user_id_type": {"open_id"}}
			if page != "" {
				query.Set("page_token", page)
			}
			if err := c.prdJSON(ctx, creds, http.MethodGet, path+"/blocks?"+query.Encode(), nil, &data); err != nil {
				var apiErr *APIError
				if attempt == 0 && errors.As(err, &apiErr) && apiErr.Code == 1770032 {
					pinnedPermissionFailure = true
					break
				}
				return snapshot, err
			}
			for _, b := range data.Items {
				if !prdToken(b.ID) {
					return snapshot, errors.New("invalid PRD block ID")
				}
				if _, exists := snapshot.Blocks[b.ID]; exists {
					return snapshot, errors.New("duplicate PRD block in pagination")
				}
				snapshot.Blocks[b.ID] = b
			}
			if len(snapshot.Blocks) > prdMaxBlocks {
				return snapshot, errors.New("PRD template exceeds block limit")
			}
			if !data.HasMore {
				break
			}
			if data.PageToken == "" || seen[data.PageToken] || len(data.Items) == 0 {
				return snapshot, errors.New("invalid PRD block pagination")
			}
			seen[data.PageToken] = true
			page = data.PageToken
		}
		if pinnedPermissionFailure {
			continue
		}
		if blockRevision != "-1" {
			return snapshot, nil
		}
		var latest struct {
			Document struct {
				ID       string `json:"document_id"`
				Revision int64  `json:"revision_id"`
			} `json:"document"`
		}
		if err := c.prdJSON(ctx, creds, http.MethodGet, path, nil, &latest); err != nil {
			return snapshot, err
		}
		if latest.Document.ID != docID || latest.Document.Revision != snapshot.Revision {
			return snapshot, errors.New("PRD document changed while reading blocks")
		}
		return snapshot, nil
	}
	return snapshot, errors.New("PRD blocks permission fallback exhausted")
}

// Matching is exact except surrounding whitespace. In particular, numbering
// and similarly named headings are not guessed or silently reconstructed.
func (s prdSnapshot) locate(heading string) (prdBlock, int, error) {
	var match prdBlock
	count := 0
	for _, b := range s.Blocks {
		if text, ok := b.heading(); ok && strings.TrimSpace(text) == strings.TrimSpace(heading) {
			match = b
			count++
		}
	}
	if count != 1 {
		return match, 0, fmt.Errorf("PRD heading %q has %d matches; expected exactly one", heading, count)
	}
	parent, ok := s.Blocks[match.Parent]
	if !ok {
		return match, 0, errors.New("PRD heading parent unavailable")
	}
	index := -1
	for i, id := range parent.Children {
		if id == match.ID {
			if index >= 0 {
				return match, 0, errors.New("PRD heading appears twice in its parent")
			}
			index = i
		}
	}
	if index < 0 {
		return match, 0, errors.New("PRD heading missing from parent children")
	}
	return match, index + 1, nil
}

func prdParagraphs(sections []PRDSection) ([][]string, error) {
	if len(sections) == 0 || len(sections) > 50 {
		return nil, errors.New("PRD requires 1-50 sections")
	}
	result := make([][]string, len(sections))
	seen := map[string]bool{}
	total := 0
	for i, section := range sections {
		heading := strings.TrimSpace(section.Heading)
		total += len(section.Body)
		if heading == "" || len(heading) > 1000 || seen[heading] || !utf8.ValidString(heading) || !utf8.ValidString(section.Body) || len(section.Body) > 20000 || total > 200000 {
			return nil, errors.New("invalid, duplicate or oversized PRD section")
		}
		seen[heading] = true
		body := strings.ReplaceAll(section.Body, "\r\n", "\n")
		if strings.TrimSpace(body) == "" {
			body = prdPendingText
		}
		for _, line := range strings.Split(body, "\n") {
			// Keep every supplied character as native text, not Markdown markup.
			start, runes := 0, 0
			for pos := range line {
				if runes == 1000 {
					result[i] = append(result[i], line[start:pos])
					start = pos
					runes = 0
				}
				runes++
			}
			result[i] = append(result[i], line[start:])
		}
		if len(result[i]) > 50 {
			return nil, errors.New("PRD section exceeds 50 native text blocks")
		}
	}
	return result, nil
}

func (s prdSnapshot) contains(match prdBlock, index int, paragraphs []string) bool {
	children := s.Blocks[match.Parent].Children
	if len(children)-index < len(paragraphs) {
		return false
	}
	for i, expected := range paragraphs {
		b, ok := s.Blocks[children[index+i]]
		text, plain := b.Text.plain()
		if !ok || b.Parent != match.Parent || b.Type != 2 || !plain || text != expected {
			return false
		}
	}
	return true
}

func (c *httpAPIClient) FillPRDDocument(ctx context.Context, creds InstallationCredentials, docID string, sections []PRDSection) error {
	paragraphs, err := prdParagraphs(sections)
	if err != nil {
		return err
	}
	snapshot, err := c.prdSnapshot(ctx, creds, docID)
	if err != nil {
		return err
	}
	// Validate all template anchors before the first mutation. Unfilled sections,
	// tables, styles, comments and pending markers are never deleted or replaced.
	for _, section := range sections {
		if _, _, err := snapshot.locate(section.Heading); err != nil {
			return err
		}
	}
	for i, section := range sections {
		match, index, err := snapshot.locate(section.Heading)
		if err != nil {
			return err
		}
		if snapshot.contains(match, index, paragraphs[i]) {
			continue
		}
		children := make([]map[string]any, 0, len(paragraphs[i]))
		for _, paragraph := range paragraphs[i] {
			children = append(children, map[string]any{"block_type": 2, "text": map[string]any{"elements": []any{map[string]any{"text_run": map[string]string{"content": paragraph}}}}})
		}
		// A stable token plus readback handles an accepted write whose response
		// was lost, including a server restart before the next Fill call.
		identity, err := json.Marshal(struct {
			Document   string
			Heading    string
			Paragraphs []string
		}{docID, match.ID, paragraphs[i]})
		if err != nil {
			return err
		}
		digest := sha256.Sum256(identity)
		clientToken := fmt.Sprintf("%x-%x-%x-%x-%x", digest[:4], digest[4:6], digest[6:8], digest[8:10], digest[10:16])
		query := url.Values{"document_revision_id": {strconv.FormatInt(snapshot.Revision, 10)}, "client_token": {clientToken}, "user_id_type": {"open_id"}}
		path := "/open-apis/docx/v1/documents/" + docID + "/blocks/" + match.Parent + "/children?" + query.Encode()
		if err := c.prdJSON(ctx, creds, http.MethodPost, path, map[string]any{"children": children, "index": index}, nil); err != nil {
			return err
		}
		snapshot, err = c.prdSnapshot(ctx, creds, docID)
		if err != nil {
			return err
		}
		match, index, err = snapshot.locate(section.Heading)
		if err != nil {
			return err
		}
		if !snapshot.contains(match, index, paragraphs[i]) {
			return fmt.Errorf("PRD section %q failed write readback", section.Heading)
		}
	}
	return nil
}

func (c *httpAPIClient) FinalizePRDOwner(ctx context.Context, creds InstallationCredentials, docID, ownerOpenID string) error {
	if !strings.HasPrefix(ownerOpenID, "ou_") || !prdToken(ownerOpenID) {
		return errors.New("PRD owner must be a real requester open_id")
	}
	meta, err := c.prdMetadata(ctx, creds, docID)
	if err != nil {
		return err
	}
	if meta.Owner == ownerOpenID {
		return nil
	}
	path := "/open-apis/drive/v1/permissions/" + docID + "/members/transfer_owner?type=docx&need_notification=false&remove_old_owner=false&old_owner_perm=edit&stay_put=false"
	if err := c.prdJSON(ctx, creds, http.MethodPost, path, map[string]string{"member_type": "openid", "member_id": ownerOpenID}, nil); err != nil {
		return err
	}
	return c.prdVerifyOwner(ctx, creds, docID, ownerOpenID)
}

func (c *httpAPIClient) prdVerifyOwner(ctx context.Context, creds InstallationCredentials, docID, ownerOpenID string) error {
	meta, err := c.prdMetadata(ctx, creds, docID)
	if err != nil {
		return err
	}
	if meta.Owner != ownerOpenID {
		return errors.New("PRD owner readback does not match original requester")
	}
	return nil
}

// VerifyPRDDocument verifies native section content and actual ownership. It
// does not claim visual/template-identical fidelity or business readiness.
func (c *httpAPIClient) VerifyPRDDocument(ctx context.Context, creds InstallationCredentials, docID string, sections []PRDSection, ownerOpenID string) error {
	if !strings.HasPrefix(ownerOpenID, "ou_") || !prdToken(ownerOpenID) {
		return errors.New("invalid PRD owner open_id")
	}
	paragraphs, err := prdParagraphs(sections)
	if err != nil {
		return err
	}
	snapshot, err := c.prdSnapshot(ctx, creds, docID)
	if err != nil {
		return err
	}
	for i, section := range sections {
		match, index, err := snapshot.locate(section.Heading)
		if err != nil {
			return err
		}
		if !snapshot.contains(match, index, paragraphs[i]) {
			return fmt.Errorf("PRD section %q not verified", section.Heading)
		}
	}
	return c.prdVerifyOwner(ctx, creds, docID, ownerOpenID)
}
