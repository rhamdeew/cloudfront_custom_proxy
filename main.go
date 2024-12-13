package main

import (
    "net/http"
    "net/http/httputil"
    "net/url"
    "regexp"
)

type customTransport struct {
    http.RoundTripper
}

func (t *customTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    // Manually set the URL to avoid encoding issues
    req.URL.Opaque = req.URL.Path
    return t.RoundTripper.RoundTrip(req)
}

func main() {
    http.HandleFunc("/s3/", func(w http.ResponseWriter, r *http.Request) {
        re := regexp.MustCompile(`^/s3/(https?)/(.*\.cloudfront\.net)/(.*)$`)
        matches := re.FindStringSubmatch(r.URL.Path)
        if len(matches) == 4 {
            scheme := matches[1]
            domain := matches[2]
            path := matches[3]

            backendURL := scheme + "://" + domain + "/" + path
            if r.URL.RawQuery != "" {
                backendURL += "?" + r.URL.RawQuery
            }

            // Manually construct the URL without using url.Parse
            remote, err := url.Parse(scheme + "://" + domain)
            if err != nil {
                http.Error(w, "Bad Gateway", http.StatusBadGateway)
                return
            }

            proxy := httputil.NewSingleHostReverseProxy(remote)
            originalDirector := proxy.Director
            proxy.Director = func(req *http.Request) {
                originalDirector(req)
                req.URL.Scheme = scheme
                req.URL.Host = domain
                req.URL.Path = "/" + path
                req.URL.RawQuery = r.URL.RawQuery
                req.Host = remote.Host
                req.Header = r.Header

                // Manually set the RequestURI to avoid encoding issues
                req.RequestURI = "/" + path
                if r.URL.RawQuery != "" {
                    req.RequestURI += "?" + r.URL.RawQuery
                }
            }

            // Use custom transport to avoid encoding issues
            proxy.Transport = &customTransport{http.DefaultTransport}

            proxy.ServeHTTP(w, r)
        } else {
            http.NotFound(w, r)
        }
    })

    http.ListenAndServe(":1337", nil)
}
