// Clients, transports, servers and request constructors from net/http.
// want: net/http.Client is not allowed here
// want: net/http.Transport is not allowed here
// want: net/http.Get is not allowed here
// want: net/http.NewRequestWithContext is not allowed here
// want: net/http.ListenAndServe is not allowed here
// want: net/http.Post is not allowed here
package fixture

import (
	"context"
	h "net/http"
)

func f() {
	c := &h.Client{Transport: &h.Transport{}}
	_, _ = h.Get("https://api.supabase.com/v1/projects")
	req, _ := h.NewRequestWithContext(context.Background(), "DELETE", "https://x", nil)
	_, _ = c.Do(req)
	_ = h.ListenAndServe(":0", nil)
	_, _ = h.Post("https://x", "", nil)
}
