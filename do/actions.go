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
	"context"
	"time"

	"github.com/digitalocean/godo"
)

// Action is a wrapper for godo.Action
type Action struct {
	*godo.Action
}

// Actions is a slice of Action.
type Actions []Action

// ActionsService is an interface for interacting with DigitalOcean's action api.
type ActionsService interface {
	List() (Actions, error)
	ListAfter(time.Time) (Actions, error)
	Get(int) (*Action, error)
}

type actionsService struct {
	client *godo.Client
}

var _ ActionsService = &actionsService{}

// NewActionsService builds an ActionsService instance.
func NewActionsService(godoClient *godo.Client) ActionsService {
	return &actionsService{
		client: godoClient,
	}
}

func (as *actionsService) listGenerator() Generator {
	return func(opt *godo.ListOptions) ([]any, *godo.Response, error) {
		list, resp, err := as.client.Actions.List(context.TODO(), opt)
		if err != nil {
			return nil, nil, err
		}

		si := make([]any, len(list))
		for i := range list {
			si[i] = list[i]
		}

		return si, resp, err
	}
}

func toActions(si []any) Actions {
	list := make(Actions, len(si))
	for i := range si {
		a := si[i].(godo.Action)
		list[i] = Action{Action: &a}
	}

	return list
}

func (as *actionsService) List() (Actions, error) {
	si, err := PaginateResp(as.listGenerator())
	if err != nil {
		return nil, err
	}

	return toActions(si), nil
}

// ListAfter returns the actions that completed at or after the given
// time without necessarily fetching every page. The actions endpoint
// only supports page/per_page (no server-side date filter or sort), so
// pages are fetched in order and pagination stops once a whole page is
// older than after: while the pages seen so far are newest-first, every
// later page is older still and cannot match. If any page breaks that
// newest-first order, early stopping is abandoned and the remaining
// pages are fetched, so an unexpected API ordering can only cost the
// old full scan, never results. Actions with no completion time are
// kept for the caller to filter, matching List.
func (as *actionsService) ListAfter(after time.Time) (Actions, error) {
	descending := true
	var prevOldest time.Time
	hasPrev := false

	shouldContinue := func(page []any) bool {
		if !descending {
			return true
		}

		times := make([]time.Time, 0, len(page))
		for _, item := range page {
			if t, ok := actionTime(item.(godo.Action)); ok {
				times = append(times, t)
			}
		}
		if len(times) == 0 {
			return true
		}

		if hasPrev && times[0].After(prevOldest) {
			descending = false
			return true
		}
		for i := 1; i < len(times); i++ {
			if times[i].After(times[i-1]) {
				descending = false
				return true
			}
		}
		prevOldest, hasPrev = times[len(times)-1], true

		// Stop when even the newest action on this page predates the
		// cutoff; continuing would only fetch older actions.
		return !times[0].Before(after)
	}

	si, err := PaginateRespWhile(as.listGenerator(), shouldContinue)
	if err != nil {
		return nil, err
	}

	out := Actions{}
	for _, a := range toActions(si) {
		if a.CompletedAt != nil && a.CompletedAt.Before(after) {
			continue
		}
		out = append(out, a)
	}

	return out, nil
}

// actionTime is the time an action is ordered and filtered by: its
// completion time, or its start time while it is still in progress.
func actionTime(a godo.Action) (time.Time, bool) {
	if a.CompletedAt != nil {
		return a.CompletedAt.Time, true
	}
	if a.StartedAt != nil {
		return a.StartedAt.Time, true
	}

	return time.Time{}, false
}

func (as *actionsService) Get(id int) (*Action, error) {
	a, _, err := as.client.Actions.Get(context.TODO(), id)
	if err != nil {
		return nil, err
	}

	return &Action{Action: a}, nil
}
