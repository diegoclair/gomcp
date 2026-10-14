package gomcp

import (
	"errors"
	"net/http"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type HandlerOptions struct {
	// Must wrap auth.ErrInvalidToken for a rejected token; any other error answers 500.
	Verifier auth.TokenVerifier
	// Full origins (scheme://host[:port]); a request carrying any other Origin gets 403.
	AllowedOrigins      []string
	ResourceMetadataURL string
	// Required behind a tunnel that forwards a public Host to a loopback listener.
	DisableLocalhostProtection bool
}

// A token without an expiration is accepted: whether it is still valid is
// the Verifier's answer.
func Handler(server *mcp.Server, opts HandlerOptions) (http.Handler, error) {
	if server == nil {
		return nil, errors.New("gomcp: server is required")
	}
	if opts.Verifier == nil {
		return nil, errors.New("gomcp: HandlerOptions.Verifier is required")
	}
	streamable := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			Stateless:                  true,
			JSONResponse:               true,
			DisableLocalhostProtection: opts.DisableLocalhostProtection,
		},
	)
	requireBearer := auth.RequireBearerToken(opts.Verifier, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL:    opts.ResourceMetadataURL,
		AllowMissingExpiration: true,
	})
	return refuseForeignOrigin(opts.AllowedOrigins, requireBearer(streamable)), nil
}

// A browser page under DNS rebinding shares the server's Host, so only an
// explicit list of origins tells a trusted page from a rebound one.
func refuseForeignOrigin(allowed []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && !slices.Contains(allowed, origin) {
			http.Error(w, "Forbidden: origin not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
