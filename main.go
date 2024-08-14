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

            backendURL := scheme + "://" + domain + "/" + path
            if r.URL.RawQuery != "" {
                backendURL += "?" + r.URL.RawQuery
            }
            remote, err := url.Parse(backendURL)
            if err != nil {
                http.Error(w, "Bad Gateway", http.StatusBadGateway)
                return
            }

            proxy := httputil.NewSingleHostReverseProxy(remote)
            originalDirector := proxy.Director
            proxy.Director = func(req *http.Request) {
                originalDirector(req)
                req.URL.Path = "/" + path
                req.URL.RawQuery = r.URL.RawQuery
                req.Host = remote.Host
                req.Header = r.Header
            }

            proxy.ServeHTTP(w, r)
        } else {
            http.NotFound(w, r)
        }
    })

    http.ListenAndServe(":1337", nil)
}
