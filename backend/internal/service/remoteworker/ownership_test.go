package remoteworker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/remotehost"
)

func TestRegisterDerivesOwnerFromACoordinatorParent(t *testing.T) {
	cases := []struct {
		name   string
		parent domain.SessionRecord
		want   domain.SessionID
	}{
		{"coordinator", domain.SessionRecord{ID: "ao-1", ProjectID: "ultra-factory", Kind: domain.KindCoordinator}, "ao-1"},
		{"root", domain.SessionRecord{ID: "ao-1", ProjectID: "ultra-factory", Kind: domain.KindOrchestrator}, ""},
		{"worker", domain.SessionRecord{ID: "ao-1", ProjectID: "ultra-factory", Kind: domain.KindWorker}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{sessions: map[domain.SessionID]domain.SessionRecord{tc.parent.ID: tc.parent}}
			svc := New(Deps{Store: store, Lookup: savedHosts(remotehost.Entry{Label: "build-box", URL: "https://build.example", HostID: "host-build"})})
			registration, err := svc.Register(context.Background(), validInput())
			if err != nil {
				t.Fatal(err)
			}
			got := registration.Link.OwnerSessionID
			if tc.want == "" {
				if got != nil {
					t.Fatalf("owner = %s, want root-owned", *got)
				}
				return
			}
			if got == nil || *got != tc.want {
				t.Fatalf("owner = %v, want %s", got, tc.want)
			}
		})
	}
}

func TestRegisterRejectsMovingALinkToAnotherProject(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	store := &fakeStore{
		sessions: map[domain.SessionID]domain.SessionRecord{
			"other-1": {ID: "other-1", ProjectID: "other", Kind: domain.KindOrchestrator},
		},
		stored: []domain.RemoteWorkerLink{{
			HostID: "host-build", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets",
			ProjectID: "ultra-factory", CreatedAt: now,
		}},
	}
	svc := New(Deps{Store: store, Lookup: savedHosts(remotehost.Entry{Label: "build-box", URL: "https://build.example", HostID: "host-build"})})
	in := validInput()
	in.ParentSessionID = "other-1"
	if _, err := svc.Register(context.Background(), in); codeOf(err) != "REMOTE_WORKER_LINK_PROJECT_CONFLICT" {
		t.Fatalf("err = %v, want REMOTE_WORKER_LINK_PROJECT_CONFLICT", err)
	}
}

func TestAssignOwnerOnARemoteLink(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	store := &fakeStore{
		sessions: map[domain.SessionID]domain.SessionRecord{
			"ao-1":    {ID: "ao-1", ProjectID: "ultra-factory", Kind: domain.KindOrchestrator},
			"ao-2":    {ID: "ao-2", ProjectID: "ultra-factory", Kind: domain.KindCoordinator},
			"other-1": {ID: "other-1", ProjectID: "other", Kind: domain.KindOrchestrator},
			"ended":   {ID: "ended", ProjectID: "ultra-factory", Kind: domain.KindCoordinator, IsTerminated: true},
		},
		stored: []domain.RemoteWorkerLink{{
			HostID: "host-build", RemoteSessionID: "widgets-3", RemoteProjectID: "widgets",
			ProjectID: "ultra-factory", CreatedAt: now,
		}},
	}
	svc := New(Deps{Store: store, Now: func() time.Time { return now }})
	ctx := context.Background()

	assigned, err := svc.AssignOwner(ctx, "host-build", "widgets-3", ptrSessionID("ao-2"), "ao-1")
	if err != nil {
		t.Fatal(err)
	}
	if assigned.OwnerSessionID == nil || *assigned.OwnerSessionID != "ao-2" {
		t.Fatalf("owner = %v", assigned.OwnerSessionID)
	}
	if len(store.ownerEvents) != 1 || store.ownerEvents[0].Reason != domain.OwnershipExplicitAssignment {
		t.Fatalf("audit = %+v", store.ownerEvents)
	}
	rooted, err := svc.AssignOwner(ctx, "host-build", "widgets-3", nil, "ao-1")
	if err != nil || rooted.OwnerSessionID != nil {
		t.Fatalf("root assignment = %+v err=%v", rooted, err)
	}

	cases := []struct {
		name        string
		owner       *domain.SessionID
		requestedBy domain.SessionID
		code        string
	}{
		{"helper may not assign", ptrSessionID("ao-2"), "ao-2", "OWNERSHIP_ROOT_ONLY"},
		{"cross-project caller", ptrSessionID("ao-2"), "other-1", "OWNERSHIP_ROOT_ONLY"},
		{"cross-project owner", ptrSessionID("other-1"), "ao-1", "OWNER_CROSS_PROJECT"},
		{"non-coordinator owner", ptrSessionID("ao-1"), "ao-1", "OWNER_NOT_COORDINATOR"},
		{"terminated owner", ptrSessionID("ended"), "ao-1", "OWNER_TERMINATED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.AssignOwner(ctx, "host-build", "widgets-3", tc.owner, tc.requestedBy)
			var apiErr *apierr.Error
			if !errors.As(err, &apiErr) || apiErr.Code != tc.code {
				t.Fatalf("err = %v, want %s", err, tc.code)
			}
		})
	}
}

func ptrSessionID(id domain.SessionID) *domain.SessionID { return &id }
