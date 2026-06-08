// auth_test_helpers.go — exported wrappers around unexported auth methods, used only in tests.
package api

import (
	"net/http"

	"github.com/vr33ni-dev/annalanah-sales-assistant-go/internal/auth"
)

// Test helpers
func (h *Handler) ParseSessionForTest(r *http.Request) (*auth.Session, bool) {
	return h.parseSession(r)
}

func (h *Handler) HandleLogoutForTest(w http.ResponseWriter, r *http.Request) {
	h.handleLogout(w, r)
}

func (h *Handler) HandleAuthStartForTest(w http.ResponseWriter, r *http.Request) {
	h.handleAuthStart(w, r)
}

func (h *Handler) HandleAuthCallbackForTest(w http.ResponseWriter, r *http.Request) {
	h.handleAuthCallback(w, r)
}

func (h *Handler) MeHandlerForTest(w http.ResponseWriter, r *http.Request) {
	h.meHandler(w, r)
}

func (h *Handler) DebugSessionForTest(w http.ResponseWriter, r *http.Request) {
	h.debugSession(w, r)
}

func (h *Handler) UserMeHandlerForTest(w http.ResponseWriter, r *http.Request) {
	h.userMeHandler(w, r)
}
