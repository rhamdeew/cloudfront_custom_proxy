package main

import (
    "net/http"
    "net/http/httputil"
    "net/url"
    "regexp"
)

func main() {
    http.HandleFunc("/s3/", func(w http.ResponseWriter, r *http.Request) {
        re := regexp.MustCompile(`^/s3/(https?)/(.*\.cloudfront\.net)/(.*)$`)
        matches := re.FindStringSubmatch(r.URL.Path)
        if len(matches) == 4 {
            scheme := matches[1]
            domain := matches[2]
            path := matches[3]

            // Construct the backend URL properly
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
                // Use RawPath to preserve encoding, fallback to Path if RawPath is empty
                if r.URL.RawPath != "" {
                    // Extract the raw path portion after /s3/scheme/domain/
                    fullRawPath := r.URL.RawPath
                    prefixPattern := "/s3/" + scheme + "/" + domain + "/"
                    if len(fullRawPath) > len(prefixPattern) {
                        req.URL.Path = "/" + fullRawPath[len(prefixPattern):]
                        req.URL.RawPath = "/" + fullRawPath[len(prefixPattern):]
                    } else {
                        req.URL.Path = "/" + path
                        req.URL.RawPath = ""
                    }
                } else {
                    req.URL.Path = "/" + path
                    req.URL.RawPath = ""
                }
                req.URL.RawQuery = r.URL.RawQuery
                req.Host = remote.Host
                req.Header = r.Header

                // Don't set RequestURI - let the proxy handle it automatically
                req.RequestURI = ""
            }

            proxy.ServeHTTP(w, r)
        } else {
            http.NotFound(w, r)
        }
    })

    http.ListenAndServe(":1337", nil)
}
