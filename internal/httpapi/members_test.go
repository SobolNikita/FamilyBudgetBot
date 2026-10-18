package httpapi

import (
	"context"
	"familyexpenses/internal/expenses"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testMembers struct {
	calls   int
	id      int64
	version int
	restore bool
}

func (m *testMembers) RemovedUsers(context.Context) ([]expenses.User, error) {
	return []expenses.User{}, nil
}
func (m *testMembers) RemoveUser(_ context.Context, id int64, version int) error {
	m.calls++
	m.id = id
	m.version = version
	return nil
}
func (m *testMembers) RestoreUser(ctx context.Context, id int64, version int) error {
	m.restore = true
	return m.RemoveUser(ctx, id, version)
}
func TestMemberRoutesValidateIdentityAndVersion(t *testing.T) {
	members := &testMembers{}
	handler := (Server{Members: members}).Handler()
	for _, sample := range []struct {
		method, path string
		status       int
	}{
		{"DELETE", "/api/users/111", 400}, {"DELETE", "/api/users/0?version=1", 400}, {"DELETE", "/api/users/111?version=-1", 400},
		{"DELETE", "/api/users/111?version=1", 204}, {"POST", "/api/users/111/restore?version=2", 204}, {"GET", "/api/users/removed", 200},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(sample.method, sample.path, nil))
		if response.Code != sample.status {
			t.Fatalf("%s %s => %d", sample.method, sample.path, response.Code)
		}
	}
	if members.calls != 2 || members.id != 111 || members.version != 2 || !members.restore {
		t.Fatalf("unexpected calls: %+v", members)
	}
	response := httptest.NewRecorder()
	respond(response, nil, expenses.ErrGone)
	if response.Code != http.StatusGone {
		t.Fatal("revoked request must return 410")
	}
}
