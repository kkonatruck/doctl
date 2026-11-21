/*
Copyright 2018 The Doctl Authors All rights reserved.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package do

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalocean/godo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newActionsTestService serves the given pages of actions (one entry per
// completed_at date, IDs assigned in serving order) and counts requests.
func newActionsTestService(t *testing.T, pages [][]string, failPage int) (ActionsService, *atomic.Int32) {
	t.Helper()

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil || page < 1 {
			page = 1
		}
		if page == failPage {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if page > len(pages) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		id := 0
		for _, p := range pages[:page-1] {
			id += len(p)
		}
		var b strings.Builder
		b.WriteString(`{"actions":[`)
		for i, date := range pages[page-1] {
			if i > 0 {
				b.WriteString(",")
			}
			id++
			fmt.Fprintf(&b, `{"id":%d,"status":"completed","type":"create","started_at":%q,"completed_at":%q,"resource_id":1,"resource_type":"droplet","region_slug":"nyc3"}`, id, date, date)
		}
		fmt.Fprintf(&b, `],"links":{"pages":{"last":"http://example.com/v2/actions?page=%d"}},"meta":{"total":%d}}`, len(pages), id)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(b.String()))
	}))
	t.Cleanup(srv.Close)

	client := godo.NewClient(nil)
	u, err := url.Parse(srv.URL + "/")
	require.NoError(t, err)
	client.BaseURL = u

	old := perPage
	perPage = 2
	t.Cleanup(func() { perPage = old })

	return NewActionsService(client), &requests
}

func actionIDs(t *testing.T, actions Actions) []int {
	t.Helper()
	ids := make([]int, len(actions))
	for i, a := range actions {
		ids[i] = a.ID
	}
	return ids
}

var newestFirstPages = [][]string{
	{"2025-04-12T00:00:00Z", "2025-04-11T12:00:00Z"},
	{"2025-04-10T00:00:00Z", "2025-04-09T00:00:00Z"},
	{"2025-03-01T00:00:00Z", "2025-02-01T00:00:00Z"},
	{"2025-01-01T00:00:00Z", "2024-12-01T00:00:00Z"},
	{"2024-11-01T00:00:00Z", "2024-10-01T00:00:00Z"},
}

// TestActionsListFetchesEveryPage is the #1683 baseline: the unfiltered
// list must request every page.
func TestActionsListFetchesEveryPage(t *testing.T) {
	svc, requests := newActionsTestService(t, newestFirstPages, 0)

	list, err := svc.List()
	require.NoError(t, err)
	assert.Len(t, list, 10)
	assert.Equal(t, int32(len(newestFirstPages)), requests.Load(), "List should request every page")
}

// TestActionsListAfterStopsEarly is the #1683 fix: with a cutoff that
// only the first page can satisfy, pagination stops after the first
// page that is entirely older than the cutoff instead of fetching all
// pages.
func TestActionsListAfterStopsEarly(t *testing.T) {
	svc, requests := newActionsTestService(t, newestFirstPages, 0)

	after := time.Date(2025, time.April, 11, 0, 0, 0, 0, time.UTC)
	list, err := svc.ListAfter(after)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, actionIDs(t, list))
	assert.Equal(t, int32(2), requests.Load(), "ListAfter should stop after the first fully-older page, not fetch all %d pages", len(newestFirstPages))
}

func TestActionsListAfterFetchesAllPagesWhenAllRecent(t *testing.T) {
	svc, requests := newActionsTestService(t, newestFirstPages, 0)

	after := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	list, err := svc.ListAfter(after)
	require.NoError(t, err)
	assert.Len(t, list, 10)
	assert.Equal(t, int32(len(newestFirstPages)), requests.Load())
}

// TestActionsListAfterFallsBackWhenNotNewestFirst guards correctness if
// the API ever returns actions oldest-first: early stopping must be
// abandoned so recent actions on later pages are not missed.
func TestActionsListAfterFallsBackWhenNotNewestFirst(t *testing.T) {
	oldestFirst := [][]string{
		{"2024-10-01T00:00:00Z", "2024-11-01T00:00:00Z"},
		{"2024-12-01T00:00:00Z", "2025-01-01T00:00:00Z"},
		{"2025-04-11T12:00:00Z", "2025-04-12T00:00:00Z"},
	}
	svc, requests := newActionsTestService(t, oldestFirst, 0)

	after := time.Date(2025, time.April, 11, 0, 0, 0, 0, time.UTC)
	list, err := svc.ListAfter(after)
	require.NoError(t, err)
	assert.Equal(t, []int{5, 6}, actionIDs(t, list))
	assert.Equal(t, int32(len(oldestFirst)), requests.Load(), "non-newest-first order must fall back to fetching every page")
}

func TestActionsListAfterReturnsPageError(t *testing.T) {
	svc, requests := newActionsTestService(t, newestFirstPages, 2)

	after := time.Date(2025, time.April, 11, 0, 0, 0, 0, time.UTC)
	_, err := svc.ListAfter(after)
	assert.Error(t, err)
	assert.Equal(t, int32(2), requests.Load())
}
