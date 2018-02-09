package server

import (
	"encoding/base64"
	"log"
	"net/http"
	"strings"
)

func (server *Server) wrapLogger(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &logResponseWriter{w, 200}
		handler.ServeHTTP(rw, r)
		log.Printf("%s %d %s %s", r.RemoteAddr, rw.status, r.Method, r.URL.Path)
	})
}

func (server *Server) wrapHeaders(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// todo add version
		w.Header().Set("Server", "GoTTY")
		handler.ServeHTTP(w, r)
	})
}

func (server *Server) wrapBasicAuth(handler http.Handler, credential string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.SplitN(r.Header.Get("Authorization"), " ", 2)

		if len(token) != 2 || strings.ToLower(token[0]) != "basic" {
			w.Header().Set("WWW-Authenticate", `Basic realm="GoTTY"`)
			http.Error(w, "Bad Request", http.StatusUnauthorized)
			return
		}

		payload, err := base64.StdEncoding.DecodeString(token[1])
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		if credential != string(payload) {
			w.Header().Set("WWW-Authenticate", `Basic realm="GoTTY"`)
			http.Error(w, "authorization failed", http.StatusUnauthorized)
			return
		}

		log.Printf("Basic Authentication Succeeded: %s", r.RemoteAddr)
		handler.ServeHTTP(w, r)
	})
}
