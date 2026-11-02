package adapters

import "testing"

// Item fields named like scan context keys must not hide the context: a
// GitHub team or Cloudflare node has a "parent" field, a Stripe object an
// "account" field.
func TestItemFieldsDoNotShadowContext(t *testing.T) {
	s := &scanner{a: &adapterSpec{}, creds: Credentials{AccountID: "acct_1", AccountName: "Acme"}}
	parent := &scanned{childCtx: map[string]any{"provider_id": "prj_1"}}
	ctx := s.itemCtx(map[string]any{"id": "x", "parent": nil, "account": "someone-else"}, parent)
	if p, ok := ctx["parent"].(map[string]any); !ok || p["provider_id"] != "prj_1" {
		t.Errorf("parent = %#v, want the for_each parent", ctx["parent"])
	}
	if ctx["account"] != "acct_1" {
		t.Errorf("account = %v, want the scanned account", ctx["account"])
	}
	if ctx["id"] != "x" {
		t.Errorf("id = %v", ctx["id"])
	}
}
