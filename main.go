package main

import (
    "log"
    "net/http"
    "net/http/httputil"
    "net/url"
    "os"
    "regexp"
    "strings"
)

var debugMode bool

func init() {
    // Enable debug mode if DEBUG environment variable is set to "true" or "1"
    debugEnv := strings.ToLower(os.Getenv("DEBUG"))
    debugMode = debugEnv == "true" || debugEnv == "1"

    if debugMode {
        log.Println("Debug mode enabled")
    }
}

func main() {
    http.HandleFunc("/s3/", func(w http.ResponseWriter, r *http.Request) {
        originalURL := r.URL.String()
        originalFullURL := "http://" + r.Host + originalURL

        if debugMode {
            log.Printf("DEBUG: Original request URL: %s", originalFullURL)
            log.Printf("DEBUG: Request path: %s", r.URL.Path)
            log.Printf("DEBUG: Request raw path: %s", r.URL.RawPath)
            log.Printf("DEBUG: Request query: %s", r.URL.RawQuery)
        }

        re := regexp.MustCompile(`^/s3/(https?)/(.*\.cloudfront\.net)/(.*)$`)
        matches := re.FindStringSubmatch(r.URL.Path)
        if len(matches) == 4 {
            scheme := matches[1]
            domain := matches[2]
            path := matches[3]

            // Construct the target URL
            targetURL := scheme + "://" + domain + "/" + path
            if r.URL.RawQuery != "" {
                targetURL += "?" + r.URL.RawQuery
            }

            if debugMode {
                log.Printf("DEBUG: Proxifying to: %s", targetURL)
                log.Printf("DEBUG: Scheme: %s, Domain: %s, Path: %s", scheme, domain, path)
            }

            // Construct the backend URL properly
            remote, err := url.Parse(scheme + "://" + domain)
            if err != nil {
                if debugMode {
                    log.Printf("DEBUG: Error parsing remote URL: %v", err)
                }
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

                if debugMode {
                    log.Printf("DEBUG: Final proxied request URL: %s://%s%s", req.URL.Scheme, req.URL.Host, req.URL.Path)
                    if req.URL.RawQuery != "" {
                        log.Printf("DEBUG: Final proxied request with query: %s://%s%s?%s", req.URL.Scheme, req.URL.Host, req.URL.Path, req.URL.RawQuery)
                    }
                }
            }

            proxy.ServeHTTP(w, r)
        } else {
            if debugMode {
                log.Printf("DEBUG: URL pattern did not match: %s", r.URL.Path)
            }
            http.NotFound(w, r)
        }
    })

    log.Println("Starting CloudFront custom proxy server on :1337")
    if debugMode {
        log.Println("Debug mode is enabled. Set DEBUG=false or unset DEBUG to disable.")
    } else {
        log.Println("Debug mode is disabled. Set DEBUG=true to enable detailed logging.")
    }

    log.Fatal(http.ListenAndServe(":1337", nil))
}
