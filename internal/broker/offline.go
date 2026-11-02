package broker

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// Responder answers a request in an offline broker: the request has passed
// every broker check (allowlist, deny list, decrypt, body pinning) and never
// reaches the network. The response still goes through the broker's size cap
// and secret stripping.
type Responder func(method, path, rawQuery string) (status int, body []byte)

// NewOffline returns a broker that answers allowed requests with respond
// instead of the network. `muretto adapter test` and fixture generation use
// it to dry-run an adapter; nothing it does can reach a provider.
func NewOffline(allow []Rule, respond Responder) *Broker {
	return New(&http.Client{Transport: offlineTransport{respond: respond}}, allow)
}

type offlineTransport struct{ respond Responder }

func (o offlineTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_ = req.Body.Close()
	}
	status, body := o.respond(req.Method, req.URL.Path, req.URL.RawQuery)
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode:    status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": {"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}, nil
}
