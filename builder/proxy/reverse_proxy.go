package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/openai/openai-go/v3"
	"opencsg.com/csghub-server/common/utils/trace"
)

type ReverseProxy interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request, api, svcHost string)
}

type reverseProxyImpl struct {
	target *url.URL
}

var DefaultResponseStreamContentType openai.ChatCompletionChunk

func NewReverseProxy(target string) (ReverseProxy, error) {
	url, err := url.Parse(target)
	if err != nil {
		return nil, err
	}
	return &reverseProxyImpl{
		target: url,
	}, nil
}

func (rp *reverseProxyImpl) ServeHTTP(w http.ResponseWriter, r *http.Request, api, svcHost string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Debug("Connection to target server interrupted", slog.Any("error", r))
		}
	}()
	proxy := httputil.NewSingleHostReverseProxy(rp.target)
	proxy.Director = func(req *http.Request) {
		if len(svcHost) > 0 {
			slog.Info("update reverse proxy header host", slog.Any("svc-host", svcHost))
			req.Host = svcHost
		} else {
			req.Host = rp.target.Host
		}
		req.URL.Host = rp.target.Host
		req.URL.Scheme = rp.target.Scheme
		if len(api) > 0 {
			// change url to given api
			req.URL.Path = api
		}

		targetQuery := rp.target.RawQuery
		if targetQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = targetQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
		}
		// dont support br comporession
		// Delete Accept-Encoding so Go's transport adds it automatically.
		// Key: if Accept-Encoding is set by user code (or forwarded from the browser),
		// Go's transport treats it as explicit and does NOT auto-decompress.
		// By deleting it here, transport adds "gzip" internally AND transparently
		// decompresses the response — so ResponseWriterWrapper receives plain text.
		req.Header.Del("Accept-Encoding")

		// Strip browser CORS/origin headers so upstream APIs (e.g. Anthropic)
		// don't treat this as a direct browser request and reject it.
		req.Header.Del("Origin")
		req.Header.Del("Referer")
		req.Header.Del("Sec-Fetch-Mode")
		req.Header.Del("Sec-Fetch-Site")
		req.Header.Del("Sec-Fetch-Dest")
		req.Header.Del("Sec-Fetch-User")
		req.Header.Del("Cookie")
		req.Header.Del("Priority")

		// debug: log outgoing request headers
		{
			slog.Info("outgoing proxy headers",
				slog.String("url", req.URL.String()),
				slog.String("x-api-key-prefix", func() string {
					k := req.Header.Get("x-api-key")
					if len(k) > 20 { return k[:20] + "..." }
					return k
				}()),
				slog.String("anthropic-version", req.Header.Get("anthropic-version")),
				slog.Int64("content-length", req.ContentLength),
				slog.String("content-type", req.Header.Get("Content-Type")),
				slog.Any("all-header-keys", func() []string {
					keys := make([]string, 0, len(req.Header))
					for k := range req.Header { keys = append(keys, k) }
					return keys
				}()),
			)
		}
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			data, _ := httputil.DumpResponse(resp, true)
			n := len(data)
			if n > 500 { n = 500 }
			slog.Error("upstream auth error", slog.Int("status", resp.StatusCode), slog.String("body", string(data[:n])))
		}
		// remove duplicated header
		resp.Header.Del("Access-Control-Allow-Credentials")
		resp.Header.Del("Access-Control-Allow-Headers")
		resp.Header.Del("Access-Control-Allow-Methods")
		resp.Header.Del("Access-Control-Allow-Origin")
		// remove duplicate X-Request-Id header from downstream response
		// because it is already set by the gateway middleware
		resp.Header.Del(trace.HeaderRequestID)

		return nil
	}
	proxy.ServeHTTP(w, r)
}
