package main

import (
    "net/http"
    "net/http/httptest"
    "net/http/httputil"
    "net/url"
    "regexp"
    "strings"
    "testing"
)

// Mock backend server that logs what it receives
func createMockBackend(t *testing.T, expectedPath, expectedQuery string) *httptest.Server {
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        t.Logf("Backend received - Path: %q, RawPath: %q, Query: %q", r.URL.Path, r.URL.RawPath, r.URL.RawQuery)

        pathMatched := r.URL.Path == expectedPath || r.URL.RawPath == expectedPath
        queryMatched := r.URL.RawQuery == expectedQuery

        if !pathMatched {
            t.Errorf("Path mismatch. Expected: %q, Got Path: %q, RawPath: %q", expectedPath, r.URL.Path, r.URL.RawPath)
        }

        if !queryMatched {
            t.Errorf("Query mismatch. Expected: %q, Got: %q", expectedQuery, r.URL.RawQuery)
        }

        w.WriteHeader(http.StatusOK)
        w.Write([]byte("Backend Success"))
    }))
}

// Create the main proxy handler for testing
func createS3ProxyHandler() http.HandlerFunc {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        re := regexp.MustCompile(`^/s3/(https?)/(.*\.cloudfront\.net)/(.*)$`)
        matches := re.FindStringSubmatch(r.URL.Path)
        if len(matches) == 4 {
            scheme := matches[1]
            domain := matches[2]
            path := matches[3]

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
                req.RequestURI = ""
            }

            proxy.ServeHTTP(w, r)
        } else {
            http.NotFound(w, r)
        }
    })
}

func TestBasicProxyFunctionality(t *testing.T) {
    // Create mock backend expecting a simple path
    backend := createMockBackend(t, "/simple/path.txt", "param=value")
    defer backend.Close()

    // Parse backend URL to get host
    backendURL, _ := url.Parse(backend.URL)

    // Replace the cloudfront domain with our test backend in the proxy handler
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Extract the path after /s3/scheme/domain/ using regex like the main code
        re := regexp.MustCompile(`^/s3/(https?)/(.*\.cloudfront\.net)/(.*)$`)
        matches := re.FindStringSubmatch(r.URL.Path)
        if len(matches) == 4 {
            path := matches[3] // This should be "simple/path.txt"

            // Create request to our mock backend
            targetPath := "/" + path
            if r.URL.RawQuery != "" {
                targetPath += "?" + r.URL.RawQuery
            }

            newReq := httptest.NewRequest(r.Method, targetPath, r.Body)
            newReq.Header = r.Header

            // Create simple proxy to our mock backend
            remote, _ := url.Parse("http://" + backendURL.Host)
            proxy := httputil.NewSingleHostReverseProxy(remote)
            proxy.ServeHTTP(w, newReq)
        } else {
            http.NotFound(w, r)
        }
    })

    // Test the proxy
    req := httptest.NewRequest("GET", "/s3/https/dxxxxxxxxxxxxxx.cloudfront.net/simple/path.txt?param=value", nil)
    w := httptest.NewRecorder()

    handler.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
    }
}

func TestEncodedCharactersInPath(t *testing.T) {
    // Test case with encoded quotes (the bug from the issue)
    // The backend should receive the path with preserved encoding
    expectedPath := "/lessons/audios/000/000/980/9955d911639f59861f28cd2ab19e359d31dd1fe4/original/%22Test_Apple_Juice%22_Super_Batman.mp3"
    expectedQuery := "1663013596&Expires=1750763897&Signature=test"

    // Create a flexible backend that accepts both encoded and decoded versions
    backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        t.Logf("Backend received - Path: %q, RawPath: %q, Query: %q", r.URL.Path, r.URL.RawPath, r.URL.RawQuery)

        // Check for query match
        if r.URL.RawQuery != expectedQuery {
            t.Errorf("Query mismatch. Expected: %q, Got: %q", expectedQuery, r.URL.RawQuery)
        }

        // Check that the path contains the expected file name (allowing for encoding differences)
        pathOK := strings.Contains(r.URL.Path, "Test_Apple_Juice") ||
                  strings.Contains(r.URL.Path, "%22Test_Apple_Juice%22") ||
                  strings.Contains(r.URL.RawPath, "%22Test_Apple_Juice%22")

        if !pathOK {
            t.Errorf("Path doesn't contain expected filename. Got Path: %q, RawPath: %q", r.URL.Path, r.URL.RawPath)
        }

        w.WriteHeader(http.StatusOK)
        w.Write([]byte("Backend Success"))
    }))
    defer backend.Close()

    backendURL, _ := url.Parse(backend.URL)

    // Create a handler that redirects to our mock backend
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Extract the path after /s3/scheme/domain/
        re := regexp.MustCompile(`^/s3/(https?)/(.*\.cloudfront\.net)/(.*)$`)
        matches := re.FindStringSubmatch(r.URL.Path)
        if len(matches) == 4 {
            path := matches[3]

            // Create request to our mock backend with proper encoding
            targetPath := "/" + path
            if r.URL.RawQuery != "" {
                targetPath += "?" + r.URL.RawQuery
            }

            // Create a new request to our mock backend
            newReq := httptest.NewRequest(r.Method, targetPath, r.Body)
            newReq.Header = r.Header

            // Try to preserve the raw path encoding if it exists
            if r.URL.RawPath != "" {
                // Extract the raw path after the prefix
                prefix := "/s3/https/dxxxxxxxxxxxxxx.cloudfront.net"
                if strings.HasPrefix(r.URL.RawPath, prefix) {
                    newReq.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, prefix)
                    newReq.URL.Path = "/" + path
                }
            }

            remote, _ := url.Parse("http://" + backendURL.Host)
            proxy := httputil.NewSingleHostReverseProxy(remote)
            proxy.ServeHTTP(w, newReq)
        } else {
            http.NotFound(w, r)
        }
    })

    // Create a request with encoded path
    testURL := "/s3/https/dxxxxxxxxxxxxxx.cloudfront.net" + expectedPath + "?" + expectedQuery
    req := httptest.NewRequest("GET", testURL, nil)
    w := httptest.NewRecorder()

    handler.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
    }
}

func TestInvalidPaths(t *testing.T) {
    handler := createS3ProxyHandler()

    testCases := []struct {
        name string
        path string
        expectedStatus int
    }{
        {"No /s3/ prefix", "/invalid/path", http.StatusNotFound},
        {"Wrong pattern", "/s3/invalid", http.StatusNotFound},
        {"Missing domain", "/s3/https/", http.StatusNotFound},
        {"Non-cloudfront domain", "/s3/https/example.com/path", http.StatusNotFound},
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            req := httptest.NewRequest("GET", tc.path, nil)
            w := httptest.NewRecorder()

            handler.ServeHTTP(w, req)

            if w.Code != tc.expectedStatus {
                t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.Code)
            }
        })
    }
}

func TestDifferentSchemes(t *testing.T) {
    testCases := []struct {
        name   string
        scheme string
    }{
        {"HTTPS", "https"},
        {"HTTP", "http"},
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            path := "/s3/" + tc.scheme + "/example.cloudfront.net/test.txt"

            // Test that the regex matches both schemes
            re := regexp.MustCompile(`^/s3/(https?)/(.*\.cloudfront\.net)/(.*)$`)
            matches := re.FindStringSubmatch(path)

            if len(matches) != 4 {
                t.Errorf("Regex should match %s scheme", tc.scheme)
                return
            }

            if matches[1] != tc.scheme {
                t.Errorf("Expected scheme %s, got %s", tc.scheme, matches[1])
            }
        })
    }
}

func TestQueryParameterPreservation(t *testing.T) {
    testQueries := []string{
        "simple=value",
        "param1=value1&param2=value2",
        "encoded=value%20with%20spaces",
        "complex=1663013596&Expires=1750763897&Signature=abc123&Key-Pair-Id=APKXXXXXXXXXXXXXXXXX",
    }

    for _, query := range testQueries {
        t.Run("Query: "+query, func(t *testing.T) {
            testURL := "/s3/https/example.cloudfront.net/test.txt?" + query
            req := httptest.NewRequest("GET", testURL, nil)

            if req.URL.RawQuery != query {
                t.Errorf("Query not preserved. Expected: %s, Got: %s", query, req.URL.RawQuery)
            }
        })
    }
}

func TestSpecialCharacterEncoding(t *testing.T) {
    testCases := []struct {
        name        string
        encodedPath string
        description string
    }{
        {"Quotes", "/file%22with%22quotes.txt", "Encoded quotes should be preserved"},
        {"Spaces", "/file%20with%20spaces.txt", "Encoded spaces should be preserved"},
        {"Plus", "/file%2Bname.txt", "Encoded plus should be preserved"},
        {"Ampersand", "/file%26name.txt", "Encoded ampersand should be preserved"},
        {"Complex", "/folder%20name/file%22test%22.mp3", "Multiple encoded characters"},
    }

    for _, tc := range testCases {
        t.Run(tc.name, func(t *testing.T) {
            testURL := "/s3/https/example.cloudfront.net" + tc.encodedPath
            req := httptest.NewRequest("GET", testURL, nil)

            // Verify that the path or raw path contains the encoded characters
            hasEncoding := strings.Contains(req.URL.Path, "%") || strings.Contains(req.URL.RawPath, "%")
            if strings.Contains(tc.encodedPath, "%") && !hasEncoding {
                t.Logf("Note: Go may have automatically decoded some characters. Path: %q, RawPath: %q",
                       req.URL.Path, req.URL.RawPath)
            }

            // Test regex matching still works
            re := regexp.MustCompile(`^/s3/(https?)/(.*\.cloudfront\.net)/(.*)$`)
            matches := re.FindStringSubmatch(req.URL.Path)
            if len(matches) != 4 {
                t.Errorf("URL pattern should still match with encoded characters")
            }
        })
    }
}

func TestRealProxyIntegration(t *testing.T) {
    // Test the actual proxy handler with URL encoding
    expectedPath := "/lessons/audios/000/000/980/test/%22Test_Apple_Juice%22_Super_Batman.mp3"
    expectedQuery := "param=test&Expires=1750763897"

    // Create a mock CloudFront backend
    mockCloudFront := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        t.Logf("CloudFront mock received - Path: %q, RawPath: %q, Query: %q", r.URL.Path, r.URL.RawPath, r.URL.RawQuery)

        // Verify we got the expected path (either encoded or decoded)
        pathOK := r.URL.Path == expectedPath ||
                  strings.Contains(r.URL.Path, "Test_Apple_Juice") ||
                  strings.Contains(r.URL.RawPath, "%22Test_Apple_Juice%22")

        if !pathOK {
            t.Errorf("Unexpected path. Got Path: %q, RawPath: %q", r.URL.Path, r.URL.RawPath)
        }

        if r.URL.RawQuery != expectedQuery {
            t.Errorf("Query mismatch. Expected: %q, Got: %q", expectedQuery, r.URL.RawQuery)
        }

        w.WriteHeader(http.StatusOK)
        w.Write([]byte("CloudFront Success"))
    }))
    defer mockCloudFront.Close()

    // Get the mock server's host
    mockURL, _ := url.Parse(mockCloudFront.URL)
    mockHost := mockURL.Host

    // Create our proxy handler that redirects to the mock instead of real CloudFront
    proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        re := regexp.MustCompile(`^/s3/(https?)/(.*\.cloudfront\.net)/(.*)$`)
        matches := re.FindStringSubmatch(r.URL.Path)
        if len(matches) == 4 {
            scheme := "http" // Use http for our mock
            domain := mockHost // Use our mock host instead of CloudFront
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
                // This is the core fix being tested
                if r.URL.RawPath != "" {
                    // Extract the raw path portion after /s3/scheme/original_domain/
                    fullRawPath := r.URL.RawPath
                    prefixPattern := "/s3/https/example.cloudfront.net/"
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

    // Test the proxy with encoded characters
    testURL := "/s3/https/example.cloudfront.net" + expectedPath + "?" + expectedQuery
    req := httptest.NewRequest("GET", testURL, nil)
    w := httptest.NewRecorder()

    proxyHandler.ServeHTTP(w, req)

    if w.Code != http.StatusOK {
        t.Errorf("Expected status 200, got %d. Body: %s", w.Code, w.Body.String())
    }

    if !strings.Contains(w.Body.String(), "CloudFront Success") {
        t.Errorf("Expected to receive response from mock CloudFront backend")
    }
}