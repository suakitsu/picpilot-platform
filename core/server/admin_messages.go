package server

import (
	"net/http"
	"strconv"
)

// Personal instances expose only their own authenticated message history. No
// platform dependency is required and a fresh personal instance has no messages.
func (s *Server) registerMessageRoutes() {
	probe, _ := http.NewRequest(http.MethodGet, "/api/messages", nil)
	if _, pattern := s.mux.Handler(probe); pattern == "GET /api/messages" {
		return
	}
	s.mux.HandleFunc("GET /api/messages", s.auth(s.messageList))
	s.mux.HandleFunc("GET /api/messages/unread-count", s.auth(s.messageUnreadCount))
	s.mux.HandleFunc("PATCH /api/messages/{id}/read", s.auth(s.messageMarkRead))
	s.mux.HandleFunc("PATCH /api/messages/{id}/unread", s.auth(s.messageMarkUnread))
	s.mux.HandleFunc("DELETE /api/messages/{id}/read", s.auth(s.messageMarkUnread))
}
func (s *Server) messageList(w http.ResponseWriter, r *http.Request) {
	f, valid := adminFilter(w, r)
	if !valid {
		return
	}
	paged := r.URL.Query().Has("page") || r.URL.Query().Has("page_size") || r.URL.Query().Has("pageSize")
	if !paged {
		f.PageSize = 100
	}
	result, err := s.Store.ListMessages(r.Context(), currentUser(r).ID, f)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	if paged {
		ok(w, r, result)
	} else {
		w.Header().Set("X-Total-Count", strconv.FormatInt(result.Total, 10))
		ok(w, r, result.Rows)
	}
}
func (s *Server) messageUnreadCount(w http.ResponseWriter, r *http.Request) {
	count, err := s.Store.UnreadMessageCount(r.Context(), currentUser(r).ID)
	if err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"unread_count": count})
}
func (s *Server) messageMarkRead(w http.ResponseWriter, r *http.Request) {
	s.messageReadState(w, r, true)
}
func (s *Server) messageMarkUnread(w http.ResponseWriter, r *http.Request) {
	s.messageReadState(w, r, false)
}
func (s *Server) messageReadState(w http.ResponseWriter, r *http.Request, read bool) {
	if err := s.Store.SetMessageRead(r.Context(), currentUser(r).ID, r.PathValue("id"), read); err != nil {
		s.adminError(w, r, err)
		return
	}
	ok(w, r, map[string]any{"message_id": r.PathValue("id"), "read": read})
}
