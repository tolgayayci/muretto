package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tolgayayci/muretto/internal/broker"
	"github.com/tolgayayci/muretto/internal/contract"
)

type scanner struct {
	a     *adapterSpec
	b     *broker.Broker
	ctx   context.Context
	creds Credentials
	now   time.Time

	byID     map[string]*scanned
	order    []*scanned
	checks   []contract.ProviderCheck
	findings map[string]contract.ExposureFinding
}

// scanned is a resource plus what templates need while the scan runs. Raw
// provider items never leave the scanner.
type scanned struct {
	res      contract.Resource
	kind     *kindSpec
	childCtx map[string]any // raw item overlaid on the resource context; children see it as parent
	parent   *scanned
}

// callError is a failed provider call, already classified.
type callError struct {
	code contract.ProviderCheckErrorCode
	msg  string
}

func (e *callError) Error() string { return e.msg }

func (s *scanner) scanKind(k *kindSpec) {
	var parents []*scanned
	if k.ForEach == "" {
		parents = []*scanned{nil}
	} else {
		for _, sc := range s.order {
			if sc.kind.Kind == k.ForEach {
				parents = append(parents, sc)
			}
		}
	}
	for _, src := range k.Sources {
		if src.RequiresOption != "" && !s.creds.Options[src.RequiresOption] {
			continue
		}
		for _, p := range parents {
			s.scanSource(k, src, p)
		}
	}
}

func (s *scanner) baseCtx(parent *scanned) map[string]any {
	ctx := map[string]any{"account": s.creds.AccountID, "account_name": s.creds.AccountName, "account_slug": s.accountSlug()}
	if parent != nil {
		ctx["parent"] = parent.childCtx
	}
	return ctx
}

func (s *scanner) accountSlug() string {
	if s.creds.AccountSlug != "" {
		return s.creds.AccountSlug
	}
	v, _ := s.a.AccountSlug.render(map[string]any{"account": s.creds.AccountID, "account_name": s.creds.AccountName})
	return v
}

func (s *scanner) scanSource(k *kindSpec, src *sourceSpec, parent *scanned) {
	var scope *string
	if parent != nil {
		sc := parent.res.ProviderID
		scope = &sc
	}
	ep := s.a.Endpoints[src.Endpoint]
	pages, err := s.fetch(ep, s.baseCtx(parent))

	produced, skipped := 0, 0
	unavailable := ""
	if err == nil {
		for _, page := range pages {
			for _, item := range itemsOf(page, src.Items) {
				ictx := s.itemCtx(item, parent)
				if src.UnavailableIf.evalItem(ictx) == triTrue {
					n, _ := lookup(item, "name")
					unavailable, _ = scalarString(n)
					if unavailable == "" {
						unavailable = "unavailable"
					}
				}
				if src.Where != nil && src.Where.eval(ictx) != triTrue {
					continue
				}
				if s.addItem(k, src, item, ictx, parent) {
					produced++
				} else {
					skipped++
				}
			}
		}
	}

	check := contract.ProviderCheck{
		Provider:    s.a.Provider,
		AccountID:   s.creds.AccountID,
		Kind:        fullKind(s.a, k.Kind),
		Scope:       scope,
		AttemptedAt: s.now,
		Status:      contract.ProviderCheckStatusOk,
	}
	var ce *callError
	switch {
	case errors.As(err, &ce):
		setCheckError(&check, ce.code, ce.msg)
	case err != nil:
		setCheckError(&check, contract.ProviderCheckErrorCodeNetwork, err.Error())
	case unavailable != "":
		setCheckError(&check, contract.ProviderCheckErrorCodeProvider, "provider reported the check unavailable: "+unavailable)
	case skipped > 0:
		setCheckError(&check, contract.ProviderCheckErrorCodeProvider, fmt.Sprintf("%d items had no id and were skipped", skipped))
	}
	if check.Status == contract.ProviderCheckStatusOk {
		t := s.now
		check.LastSuccessAt = &t
	}
	if src.SeenVia == "list" {
		c := check
		n := produced
		c.Items = &n
		s.checks = append(s.checks, c)
	}
	for _, rule := range src.ChecksRules {
		c := check
		id := rule
		c.RuleID = &id
		s.checks = append(s.checks, c)
	}
}

func setCheckError(c *contract.ProviderCheck, code contract.ProviderCheckErrorCode, msg string) {
	c.Status = contract.ProviderCheckStatusError
	c.ErrorCode = &code
	m := truncate(msg, 500)
	c.Error = &m
}

// evalItem is eval for optional item predicates (nil means "no").
func (p *predicate) evalItem(ctx map[string]any) tri {
	if p == nil {
		return triFalse
	}
	return p.eval(ctx)
}

func (s *scanner) itemCtx(item map[string]any, parent *scanned) map[string]any {
	ctx := make(map[string]any, len(item)+3)
	for k, v := range item {
		ctx[k] = v
	}
	for k, v := range s.baseCtx(parent) {
		if _, taken := ctx[k]; !taken {
			ctx[k] = v
		}
	}
	return ctx
}

func (s *scanner) addItem(k *kindSpec, src *sourceSpec, item, ictx map[string]any, parent *scanned) bool {
	pid, ok := src.ID.render(ictx)
	if !ok {
		return false
	}
	res := contract.Resource{
		ID:          fullKind(s.a, k.Kind) + ":" + pid,
		Provider:    s.a.Provider,
		AccountID:   s.creds.AccountID,
		Kind:        fullKind(s.a, k.Kind),
		ProviderID:  truncate(pid, 300),
		Existence:   contract.ResourceExistenceConfirmed,
		SeenVia:     contract.ResourceSeenVia(src.SeenVia),
		FirstSeenAt: s.now,
		LastSeenAt:  s.now,
		Attributes:  contract.ResourceAttributes{},
	}
	if s.creds.AccountName != "" {
		n := s.creds.AccountName
		res.AccountName = &n
	}
	res.Name = pid
	if n, ok := src.Name.render(ictx); ok {
		res.Name = truncate(n, 300)
	}
	// A kind without a status or url template renders "": leave them unset.
	if v, ok := src.Status.render(ictx); ok && v != "" {
		st := truncate(v, 50)
		res.Status = &st
	}
	if v, ok := src.URL.render(ictx); ok && v != "" {
		u := truncate(v, 1000)
		res.Url = &u
	}
	switch {
	case !src.Parent.empty():
		if v, ok := src.Parent.render(ictx); ok {
			p := fullKind(s.a, src.ParentKind) + ":" + v
			res.ParentID = &p
		}
	case parent != nil:
		p := parent.res.ID
		res.ParentID = &p
	}
	if src.CreatedAt != nil {
		if v, ok := src.CreatedAt.Value.render(ictx); ok {
			if t, ok := parseTime(v, src.CreatedAt.Format); ok {
				res.CreatedAt = &t
			}
		}
	}
	applyAttrs(res.Attributes, src.Attributes, ictx)
	if slug := s.accountSlug(); slug != "" {
		res.Attributes["account_slug"] = slug
	}

	if prev := s.byID[res.ID]; prev != nil {
		// Same resource from a second source: keep the first values, add new
		// attributes, and prefer "list" over "advisor".
		for k, v := range res.Attributes {
			if _, ok := prev.res.Attributes[k]; !ok {
				prev.res.Attributes[k] = v
			}
		}
		if res.SeenVia == contract.ResourceSeenViaList {
			prev.res.SeenVia = res.SeenVia
		}
		prev.childCtx = overlay(resourceCtx(prev.res, nil), item)
		return true
	}

	sc := &scanned{res: res, kind: k, parent: parent}
	for _, e := range k.Enrich {
		if e.RequiresOption != "" && !s.creds.Options[e.RequiresOption] {
			continue
		}
		pages, err := s.fetch(s.a.Endpoints[e.Endpoint], ictx)
		if err != nil || len(pages) == 0 {
			continue // missing attributes make dependent rules unknown
		}
		if obj, ok := pages[0].(map[string]any); ok {
			ectx := s.itemCtx(obj, parent)
			applyAttrs(sc.res.Attributes, e.Attributes, ectx)
		}
	}
	sc.childCtx = overlay(resourceCtx(sc.res, nil), item)
	s.byID[res.ID] = sc
	s.order = append(s.order, sc)
	return true
}

func applyAttrs(dst contract.ResourceAttributes, attrs []attrSpec, ctx map[string]any) {
	for _, at := range attrs {
		if at.Field != "" {
			if v, found := lookup(ctx, at.Field); found {
				if av, ok := attrValue(v); ok {
					if av == nil && at.Default != nil {
						av = *at.Default
					}
					dst[at.Name] = av
					continue
				}
			}
		} else if v, ok := at.Template.render(ctx); ok {
			dst[at.Name] = v
			continue
		}
		if at.Default != nil {
			dst[at.Name] = *at.Default
		}
	}
}

func overlay(base map[string]any, raw map[string]any) map[string]any {
	for k, v := range raw {
		base[k] = v
	}
	return base
}

// resourceCtx is what rules, fixes, remove hints and match rules see:
// attributes at top level, core fields under reserved names, and the parent's
// context under "parent" (only during a scan).
func resourceCtx(res contract.Resource, parent map[string]any) map[string]any {
	ctx := make(map[string]any, len(res.Attributes)+12)
	for k, v := range res.Attributes {
		ctx[k] = v
	}
	ctx["id"] = res.ID
	ctx["provider_id"] = res.ProviderID
	ctx["name"] = res.Name
	ctx["kind"] = res.Kind
	ctx["account_id"] = res.AccountID
	if res.AccountName != nil {
		ctx["account_name"] = *res.AccountName
	}
	if res.Url != nil {
		ctx["url"] = *res.Url
	}
	if res.Status != nil {
		ctx["status"] = *res.Status
	}
	if res.ParentID != nil {
		ctx["parent_id"] = *res.ParentID
	}
	if parent != nil {
		ctx["parent"] = parent
	}
	return ctx
}

func (s *scanner) evaluateRules() {
	s.findings = map[string]contract.ExposureFinding{}
	for _, rule := range s.a.Rules {
		for _, sc := range s.order {
			if sc.kind.Kind != rule.Kind {
				continue
			}
			var pctx map[string]any
			if sc.parent != nil {
				pctx = sc.parent.childCtx
			}
			ctx := resourceCtx(sc.res, pctx)
			if rule.Applies.eval(ctx) != triTrue {
				continue
			}
			state := contract.ExposureFindingStateUnknown
			exp := rule.Exposed.eval(ctx)
			notExp := triUnknown
			if rule.NotExposed != nil {
				notExp = rule.NotExposed.eval(ctx)
			} else if exp != triUnknown {
				notExp = triOf(exp == triFalse)
			}
			switch {
			case exp == triTrue:
				state = contract.ExposureFindingStateExposed
			case notExp == triTrue:
				state = contract.ExposureFindingStateNotExposed
			}
			f := contract.ExposureFinding{
				ID:          rule.ID + ":" + sc.res.ID,
				ResourceID:  sc.res.ID,
				RuleID:      rule.ID,
				State:       state,
				Severity:    contract.ExposureFindingSeverity(rule.Severity),
				Signal:      contract.ExposureFindingSignal(rule.Signal),
				Title:       truncate(rule.Title.renderLoose(ctx), 200),
				Explanation: truncate(rule.Explanation.renderLoose(ctx), 1000),
				Fix:         renderFix(rule.Fix, ctx),
			}
			// The adapter's title, explanation and fix describe the exposed
			// state; other states must not repeat that claim.
			switch state {
			case contract.ExposureFindingStateNotExposed:
				f.Title = NotExposedTitle(rule.ID)
				f.Explanation = NotExposedExplanation
				f.Fix = NothingToFix()
			case contract.ExposureFindingStateUnknown:
				f.Title = UnknownTitle(rule.ID)
				f.Explanation = UnknownExplanation
			case contract.ExposureFindingStateExposed:
			}
			if rule.Caveat != "" {
				c := truncate(strings.TrimSpace(rule.Caveat), 500)
				f.Caveat = &c
			}
			if state != contract.ExposureFindingStateUnknown {
				t := s.now
				f.CheckedAt = &t
			}
			if prev, ok := s.findings[f.ID]; ok && stateRank(prev.State) >= stateRank(f.State) {
				continue
			}
			s.findings[f.ID] = f
		}
	}
}

// stateRank orders duplicate findings for one resource and rule (e.g. advisor
// and catalog both ran): exposed wins, then unknown, then not_exposed.
func stateRank(s contract.ExposureFindingState) int {
	switch s {
	case contract.ExposureFindingStateExposed:
		return 3
	case contract.ExposureFindingStateUnknown:
		return 2
	case contract.ExposureFindingStateNotExposed:
		return 1
	}
	return 0
}

func (s *scanner) result() ScanResult {
	out := ScanResult{Checks: s.checks}
	for _, sc := range s.order {
		out.Resources = append(out.Resources, sc.res)
	}
	ids := make([]string, 0, len(s.findings))
	for id := range s.findings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out.Findings = append(out.Findings, s.findings[id])
	}
	return out
}

// fetch calls one endpoint through the broker, following pagination, and
// returns the decoded JSON of every page.
func (s *scanner) fetch(ep *endpointSpec, ctx map[string]any) ([]any, error) {
	base := "https://" + s.a.API.host // canonical form, checked at load time
	path := ep.Path
	for name, t := range ep.Params {
		v, ok := t.render(ctx)
		if !ok {
			return nil, &callError{contract.ProviderCheckErrorCodeUnsupported, fmt.Sprintf("%s %s: no value for {%s}", ep.Method, ep.Path, name)}
		}
		path = strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(v))
	}
	q := url.Values{}
	for name, t := range ep.Query {
		if v, ok := t.render(ctx); ok {
			q.Set(name, v)
		}
	}
	var body []byte
	if ep.PinnedBody != "" {
		body, _, _ = broker.PinnedBody(ep.PinnedBody)
	}

	var pages []any
	seen := map[string]bool{}
	for page := 0; ; page++ {
		u := base + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		doc, empty, err := s.call(ep, u, body)
		if err != nil {
			return nil, err
		}
		if empty {
			return pages, nil
		}
		pages = append(pages, doc)
		if ep.Pagination == nil {
			return pages, nil
		}
		nv, _ := lookup(doc, ep.Pagination.Next)
		next, ok := scalarString(nv)
		if !ok || next == "" {
			return pages, nil
		}
		if seen[next] {
			return nil, &callError{contract.ProviderCheckErrorCodeProvider, fmt.Sprintf("%s %s: pagination cursor repeated", ep.Method, ep.Path)}
		}
		seen[next] = true
		if page+1 >= ep.Pagination.MaxPages {
			return nil, &callError{contract.ProviderCheckErrorCodeProvider, fmt.Sprintf("%s %s: more than %d pages; results would be incomplete", ep.Method, ep.Path, ep.Pagination.MaxPages)}
		}
		q.Set(ep.Pagination.Param, next)
	}
}

func (s *scanner) call(ep *endpointSpec, rawURL string, body []byte) (doc any, empty bool, err error) {
	where := ep.Method + " " + ep.Path
	req, err := broker.NewRequest(s.ctx, ep.Method, rawURL, body)
	if err != nil {
		return nil, false, &callError{contract.ProviderCheckErrorCodeUnsupported, where + ": " + err.Error()}
	}
	if s.creds.Token != "" {
		v := s.creds.Token
		if s.a.API.Auth.Scheme != "" {
			v = s.a.API.Auth.Scheme + " " + v
		}
		req.Header.Set(s.a.API.Auth.Header, v)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "muretto")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.b.Do(s.ctx, req)
	if err != nil {
		code := contract.ProviderCheckErrorCodeNetwork
		switch {
		case errors.Is(err, broker.ErrRefused):
			code = contract.ProviderCheckErrorCodeUnsupported
		case errors.Is(err, broker.ErrTooLarge):
			code = contract.ProviderCheckErrorCodeProvider
		}
		return nil, false, &callError{code, where + ": " + stripURLs(err.Error())}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, &callError{contract.ProviderCheckErrorCodeNetwork, where + ": read failed"}
	}
	if ep.OnStatus[resp.StatusCode] == "empty" {
		return nil, true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, false, &callError{statusCode(resp.StatusCode), fmt.Sprintf("%s: HTTP %d", where, resp.StatusCode)}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, false, &callError{contract.ProviderCheckErrorCodeProvider, where + ": response is not JSON"}
	}
	return doc, false, nil
}

func statusCode(status int) contract.ProviderCheckErrorCode {
	switch {
	case status == http.StatusUnauthorized:
		return contract.ProviderCheckErrorCodeAuth
	case status == http.StatusForbidden:
		return contract.ProviderCheckErrorCodePermission
	case status == http.StatusNotFound:
		return contract.ProviderCheckErrorCodeNotFound
	case status == http.StatusTooManyRequests:
		return contract.ProviderCheckErrorCodeRateLimit
	default:
		return contract.ProviderCheckErrorCodeProvider
	}
}

// stripURLs keeps error text free of URLs (they may carry ids we do not want
// in check messages; tokens are never in URLs).
func stripURLs(s string) string {
	fields := strings.Fields(s)
	for i, f := range fields {
		if strings.Contains(f, "://") {
			fields[i] = "<url>"
		}
	}
	return strings.Join(fields, " ")
}

// itemsOf returns the item objects of a response. Each alternative path is
// tried in order; an array yields its objects, an object yields itself ("$"
// on a bare object, as Vercel's env list sometimes returns).
func itemsOf(doc any, paths []string) []map[string]any {
	if len(paths) == 0 {
		paths = []string{"$"}
	}
	for _, p := range paths {
		v, ok := lookup(doc, p)
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case []any:
			var out []map[string]any
			for _, el := range t {
				if m, ok := el.(map[string]any); ok {
					out = append(out, m)
				}
			}
			return out
		case map[string]any:
			if p == "$" || len(paths) == 1 {
				return []map[string]any{t}
			}
		}
	}
	return nil
}

func parseTime(v, format string) (time.Time, bool) {
	parseNum := func() (float64, bool) {
		f, err := strconv.ParseFloat(v, 64)
		return f, err == nil
	}
	switch format {
	case "rfc3339":
		t, err := time.Parse(time.RFC3339Nano, v)
		return t.UTC(), err == nil
	case "epoch_ms":
		f, ok := parseNum()
		return time.UnixMilli(int64(f)).UTC(), ok
	case "epoch_s":
		f, ok := parseNum()
		return time.Unix(int64(f), 0).UTC(), ok
	case "auto":
		if f, ok := parseNum(); ok {
			if f > 1e11 { // later than 1973 in ms, or year 5138 in seconds
				return time.UnixMilli(int64(f)).UTC(), true
			}
			return time.Unix(int64(f), 0).UTC(), true
		}
		t, err := time.Parse(time.RFC3339Nano, v)
		return t.UTC(), err == nil
	}
	return time.Time{}, false
}
