// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	meerrors "github.com/larksuite/meegle-cli/internal/products/meegle/errors"
)

const successHTML = `<!DOCTYPE html><html><body><h1>Authorization Successful!</h1><p>You can close this page and return to the terminal.</p></body></html>`
const errorHTML = `<!DOCTYPE html><html><body><h1>Authorization Error</h1><p>State verification failed, please try again.</p></body></html>`

type CallbackResult struct{ Code string }

type CallbackServer struct {
	RedirectURI string
	listener    net.Listener
	server      *http.Server
	resultCh    chan callbackOutcome
}

type callbackOutcome struct {
	result *CallbackResult
	err    error
}

func StartCallbackServer() (*CallbackServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	cs := &CallbackServer{
		RedirectURI: fmt.Sprintf("http://127.0.0.1:%d/callback", port),
		listener:    listener,
		resultCh:    make(chan callbackOutcome, 1),
	}
	cs.server = &http.Server{}
	return cs, nil
}

func (cs *CallbackServer) WaitForCallback(expectedState string) (*CallbackResult, error) {
	mux := http.NewServeMux()
	cs.server.Handler = mux
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		u, _ := url.Parse(r.RequestURI)
		code := u.Query().Get("code")
		state := u.Query().Get("state")
		if state != expectedState {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(400)
			_, _ = w.Write([]byte(errorHTML))
			cs.resultCh <- callbackOutcome{err: meerrors.NewClientError("OAUTH_STATE_MISMATCH", "OAuth state mismatch")}
			return
		}
		if code == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(400)
			_, _ = w.Write([]byte(errorHTML))
			cs.resultCh <- callbackOutcome{err: meerrors.NewClientError("OAUTH_NO_CODE", "No authorization code received")}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(successHTML))
		cs.resultCh <- callbackOutcome{result: &CallbackResult{Code: code}}
	})
	go func() { _ = cs.server.Serve(cs.listener) }()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	select {
	case outcome := <-cs.resultCh:
		return outcome.result, outcome.err
	case <-ctx.Done():
		return nil, meerrors.NewClientError("OAUTH_TIMEOUT", "Authorization timed out (120 seconds)")
	}
}

func (cs *CallbackServer) Close() { cs.server.Close() }
