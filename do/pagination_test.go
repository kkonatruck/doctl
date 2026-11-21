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
	"errors"
	"sync"
	"testing"

	"github.com/digitalocean/godo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_PaginateResp(t *testing.T) {
	var mu sync.Mutex
	currentPage := 0
	resp := &godo.Response{Links: &godo.Links{Pages: &godo.Pages{Last: "http://example.com/?page=5"}}}

	gen := func(*godo.ListOptions) ([]any, *godo.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		currentPage++
		return []any{currentPage}, resp, nil
	}

	list, err := PaginateResp(gen)
	assert.NoError(t, err)

	assert.Len(t, list, 5)
}

func Test_PaginateRespWhileStopsEarly(t *testing.T) {
	resp := &godo.Response{Links: &godo.Links{Pages: &godo.Pages{Last: "http://example.com/?page=5"}}}
	calls := 0
	gen := func(opt *godo.ListOptions) ([]any, *godo.Response, error) {
		calls++
		return []any{opt.Page}, resp, nil
	}

	list, err := PaginateRespWhile(gen, func(page []any) bool {
		return page[0].(int) < 2
	})
	require.NoError(t, err)
	assert.Equal(t, []any{1, 2}, list, "the page that stops pagination is included")
	assert.Equal(t, 2, calls, "no page after the stopping page should be requested")
}

func Test_PaginateRespWhileReturnsPageError(t *testing.T) {
	resp := &godo.Response{Links: &godo.Links{Pages: &godo.Pages{Last: "http://example.com/?page=5"}}}
	calls := 0
	gen := func(opt *godo.ListOptions) ([]any, *godo.Response, error) {
		calls++
		if opt.Page == 2 {
			return nil, nil, errors.New("page failed")
		}
		return []any{opt.Page}, resp, nil
	}

	_, err := PaginateRespWhile(gen, func([]any) bool { return true })
	assert.ErrorContains(t, err, "page failed")
	assert.Equal(t, 2, calls)
}

func Test_Pagination_fetchPage(t *testing.T) {
	gen := func(opt *godo.ListOptions) ([]any, *godo.Response, error) {
		items := []any{}
		resp := &godo.Response{}

		assert.Equal(t, 10, opt.Page)

		return items, resp, nil
	}

	fetchPage(gen, 10)
}

func Test_Pagination_lastPage(t *testing.T) {
	cases := []struct {
		r        *godo.Response
		lastPage int
		isValid  bool
	}{
		{
			r: &godo.Response{
				Links: &godo.Links{
					Pages: &godo.Pages{Last: "http://example.com/?page=1"},
				},
			},
			lastPage: 1,
			isValid:  true,
		},
		{
			r:        &godo.Response{Links: &godo.Links{}},
			lastPage: 1,
			isValid:  true,
		},

		{
			r:        &godo.Response{Links: nil},
			lastPage: 1,
			isValid:  true,
		},
	}

	for _, c := range cases {
		lp, err := lastPage(c.r)
		if c.isValid {
			assert.NoError(t, err)
			assert.Equal(t, c.lastPage, lp)
		} else {
			assert.Error(t, err)
		}
	}
}

func Test_PaginateResp_emptyListWithZeroLastPage(t *testing.T) {
	// Regression test for https://github.com/digitalocean/doctl/issues/1888:
	// a Last page link whose page param parses to 0 (as with an empty
	// collection) made lastPage return 0, so set(1, ...) panicked indexing
	// a zero-length page list. lastPage clamps such values to 1.
	resp := &godo.Response{Links: &godo.Links{Pages: &godo.Pages{Last: "http://example.com/?page=0"}}}

	gen := func(*godo.ListOptions) ([]any, *godo.Response, error) {
		return []any{}, resp, nil
	}

	list, err := PaginateResp(gen)
	assert.NoError(t, err)
	assert.Empty(t, list)
}
