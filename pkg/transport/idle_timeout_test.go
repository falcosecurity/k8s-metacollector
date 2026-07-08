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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// streamHandler writes a chunk every interval for count chunks, then goes
// silent until the client disconnects.
func streamHandler(interval time.Duration, count int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		for range count {
			time.Sleep(interval)
			if _, err := w.Write([]byte("event\n")); err != nil {
				return
			}
			flusher.Flush()
		}
		// Go silent, keeping the connection open.
		<-r.Context().Done()
	}
}

func get(t *testing.T, timeout time.Duration, url string) *http.Response {
	t.Helper()
	client := &http.Client{Transport: NewWatchIdleTimeoutWrapper(timeout)(http.DefaultTransport)}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	return resp
}

func TestWatchStreamTimesOutWhenIdle(t *testing.T) {
	srv := httptest.NewServer(streamHandler(10*time.Millisecond, 1))
	defer srv.Close()

	resp := get(t, 200*time.Millisecond, srv.URL+"?watch=true")
	defer resp.Body.Close()

	start := time.Now()
	data, err := io.ReadAll(resp.Body)
	require.Error(t, err)
	require.ErrorContains(t, err, "watch stream received no data")
	// The chunk sent before the stream went idle must have been received.
	require.Equal(t, "event\n", string(data))
	// The read must have failed roughly at the idle timeout, not immediately
	// and not at some larger overall deadline.
	require.WithinDuration(t, start.Add(230*time.Millisecond), time.Now(), 150*time.Millisecond)
}

func TestWatchStreamStaysOpenWhileDataFlows(t *testing.T) {
	// Chunks arrive every 50ms, idle timeout is 200ms: the timer must be
	// re-armed on every read and the stream must survive well past the
	// timeout as long as data keeps flowing.
	srv := httptest.NewServer(streamHandler(50*time.Millisecond, 10))
	defer srv.Close()

	resp := get(t, 200*time.Millisecond, srv.URL+"?watch=true")
	defer resp.Body.Close()

	buf := make([]byte, 6)
	received := 0
	for received < 10 {
		_, err := io.ReadFull(resp.Body, buf)
		require.NoError(t, err)
		received++
	}
}

func TestNonWatchRequestsAreNotWrapped(t *testing.T) {
	srv := httptest.NewServer(streamHandler(10*time.Millisecond, 2))
	defer srv.Close()

	resp := get(t, 200*time.Millisecond, srv.URL)
	defer resp.Body.Close()
	require.IsType(t, &http.Response{}, resp)
	_, wrapped := any(resp.Body).(*idleTimeoutBody)
	require.False(t, wrapped)
}

func TestExplicitCloseStopsTimer(t *testing.T) {
	srv := httptest.NewServer(streamHandler(10*time.Millisecond, 1))
	defer srv.Close()

	resp := get(t, 50*time.Millisecond, srv.URL+"?watch=true")
	require.NoError(t, resp.Body.Close())
	// Give a fired timer a chance to run; closing again must not panic and
	// the body must not report a timeout.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, resp.Body.Close())
}
