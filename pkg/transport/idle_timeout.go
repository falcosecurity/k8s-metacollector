// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 The Falco Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package transport

import (
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// NewWatchIdleTimeoutWrapper returns a wrapper for an http.RoundTripper that
// enforces an idle timeout on the response body of watch requests. If the API
// server sends no data on a watch stream for longer than the timeout, the
// stream is closed and the pending read fails, which causes the reflector to
// re-establish the watch.
//
// This protects against watch streams that die silently: the TCP connection
// stays healthy (HTTP/2 pings are answered), the server never closes the
// response, but no events are delivered anymore. Informers survive such
// streams in a degraded mode where updates are lost, which for this collector
// means metadata for new pods is never delivered to subscribers.
//
// The API server sends bookmark events on watches roughly every minute
// (reflectors request them by default), so a healthy watch stream is never
// silent for more than a couple of minutes regardless of resource activity.
func NewWatchIdleTimeoutWrapper(timeout time.Duration) func(http.RoundTripper) http.RoundTripper {
	return func(rt http.RoundTripper) http.RoundTripper {
		return &watchIdleTimeoutRoundTripper{delegate: rt, timeout: timeout}
	}
}

type watchIdleTimeoutRoundTripper struct {
	delegate http.RoundTripper
	timeout  time.Duration
}

// RoundTrip implements http.RoundTripper, wrapping the response body of
// successful watch requests with an idle timeout.
func (w *watchIdleTimeoutRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := w.delegate.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	if resp.StatusCode == http.StatusOK && isWatchRequest(req) {
		resp.Body = newIdleTimeoutBody(resp.Body, w.timeout)
	}
	return resp, nil
}

// WrappedRoundTripper returns the delegate round tripper. It implements
// k8s.io/apimachinery/pkg/util/net.RoundTripperWrapper so that client-go can
// unwrap the transport when needed.
func (w *watchIdleTimeoutRoundTripper) WrappedRoundTripper() http.RoundTripper {
	return w.delegate
}

func isWatchRequest(req *http.Request) bool {
	return req.URL.Query().Get("watch") == "true"
}

// idleTimeoutBody closes the underlying response body if no data is received
// for longer than the timeout. The timer is re-armed on every successful read.
type idleTimeoutBody struct {
	rc       io.ReadCloser
	timer    *time.Timer
	timeout  time.Duration
	timedOut atomic.Bool
}

func newIdleTimeoutBody(rc io.ReadCloser, timeout time.Duration) *idleTimeoutBody {
	b := &idleTimeoutBody{rc: rc, timeout: timeout}
	b.timer = time.AfterFunc(timeout, func() {
		b.timedOut.Store(true)
		// Closing the body unblocks any pending Read with an error.
		_ = b.rc.Close()
	})
	return b
}

// Read implements io.Reader.
func (b *idleTimeoutBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if err != nil {
		if b.timedOut.Load() {
			return n, fmt.Errorf("watch stream received no data for %s, closing it: %w", b.timeout, err)
		}
		return n, err
	}
	b.timer.Reset(b.timeout)
	return n, nil
}

// Close implements io.Closer.
func (b *idleTimeoutBody) Close() error {
	b.timer.Stop()
	return b.rc.Close()
}
