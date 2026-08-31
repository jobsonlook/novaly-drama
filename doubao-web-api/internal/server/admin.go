package server

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mask/ai/doubao-web-api/internal/account"
	"github.com/mask/ai/doubao-web-api/internal/pool"
)

//go:embed templates/*.html
var adminTemplatesFS embed.FS

var adminTmpl = template.Must(template.ParseFS(adminTemplatesFS, "templates/*.html"))

type adminPageData struct {
	Accounts          []account.Account
	Active            *account.Account
	ActiveID          int64
	Workers           []pool.WorkerStatus
	ActiveSessionFile string
	NeedsRestart      bool
	Flash             string
	Error             string
	AuthKey           string // DOUBAO_API_KEY for form posts when auth is enabled
	DefaultRemaining  int
	Generations       []account.Generation
	HistoryTotal      int
	HistoryPage       int
	HistoryPageSize   int
	HistoryTotalPages int
	HistoryHasPrev    bool
	HistoryHasNext    bool
	HistoryPrevPage   int
	HistoryNextPage   int
	HistoryFrom       int
	HistoryTo         int
}

func (s *Server) registerAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin", s.handleAdminIndex)
	mux.HandleFunc("GET /admin/history/media/{name}", s.handleAdminHistoryMedia)
	mux.HandleFunc("POST /admin/accounts", s.handleAdminCreate)
	mux.HandleFunc("POST /admin/accounts/{id}/select", s.handleAdminSelect)
	mux.HandleFunc("POST /admin/accounts/{id}/reset", s.handleAdminReset)
	mux.HandleFunc("POST /admin/accounts/{id}/delete", s.handleAdminDelete)
	mux.HandleFunc("POST /admin/accounts/{id}/update", s.handleAdminUpdate)
}

func (s *Server) checkAdminAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.cfg.APIKey == "" {
		return true
	}
	// Bearer header (API clients) or ?key= / form key for browser forms.
	if err := s.checkAuth(r); err == nil {
		return true
	}
	key := r.URL.Query().Get("key")
	if key == "" {
		key = r.FormValue("key")
	}
	if key != "" && key == s.cfg.APIKey {
		return true
	}
	http.Error(w, "unauthorized: set Authorization: Bearer <DOUBAO_API_KEY> or ?key=", http.StatusUnauthorized)
	return false
}

func (s *Server) handleAdminIndex(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminAuth(w, r) {
		return
	}
	flash := r.URL.Query().Get("flash")
	errMsg := r.URL.Query().Get("error")
	s.renderAdmin(w, r, flash, errMsg)
}

func (s *Server) handleAdminCreate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminAuth(w, r) {
		return
	}
	if s.accounts == nil {
		s.redirectAdmin(w, r, "", "account store not configured")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.redirectAdmin(w, r, "", "invalid form")
		return
	}
	remaining := account.DefaultFastRemaining
	if v := strings.TrimSpace(r.FormValue("fast_remaining")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			s.redirectAdmin(w, r, "", "invalid fast_remaining")
			return
		}
		remaining = n
	}
	a, err := s.accounts.Create(r.FormValue("name"), r.FormValue("session_dir"), remaining)
	if err != nil {
		s.redirectAdmin(w, r, "", err.Error())
		return
	}
	s.redirectAdmin(w, r, fmt.Sprintf("已添加账号 %s", a.Name), "")
}

func (s *Server) handleAdminSelect(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminAuth(w, r) {
		return
	}
	if s.accounts == nil {
		s.redirectAdmin(w, r, "", "account store not configured")
		return
	}
	id, err := parsePathID(r)
	if err != nil {
		s.redirectAdmin(w, r, "", "invalid id")
		return
	}
	a, err := s.accounts.Select(id)
	if err != nil {
		s.redirectAdmin(w, r, "", err.Error())
		return
	}
	if !s.cfg.AutoRestartChrome {
		s.markSessionSwitchPending()
		s.redirectAdmin(w, r, fmt.Sprintf("已选用 %s，请重启 Chrome 以加载 %s", a.Name, a.SessionDir), "")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	// Multi-Chrome pool: open/restart the worker for this account (rebind idle
	// slot if the account is not currently warmed).
	if s.workers != nil && s.workers.Len() > 0 {
		if err := s.workers.OpenAccount(ctx, a); err != nil {
			s.markSessionSwitchPending()
			s.redirectAdmin(w, r, "", fmt.Sprintf("已选用 %s，但自动打开 Chrome 失败: %v", a.Name, err))
			return
		}
		// Opening/rebinding Chrome can take several seconds. Re-assert the
		// selection after it succeeds so a concurrent quota/update request cannot
		// leave the admin "current account" pointing at a different session than
		// the Chrome worker we just opened.
		if _, err := s.accounts.Select(a.ID); err != nil {
			s.markSessionSwitchPending()
			s.redirectAdmin(w, r, "", fmt.Sprintf("Chrome 已打开 %s，但同步当前账号失败: %v", a.Name, err))
			return
		}
		s.sessionSwitchPending.Store(false)
		s.redirectAdmin(w, r, fmt.Sprintf("已选用 %s 并自动打开 Chrome（%s）", a.Name, a.SessionDir), "")
		return
	}

	if s.chrome != nil {
		if err := s.chrome.Restart(ctx); err != nil {
			s.markSessionSwitchPending()
			s.redirectAdmin(w, r, "", fmt.Sprintf("已选用 %s，但自动重启 Chrome 失败: %v（请手动执行 ./scripts/start-chrome.sh）", a.Name, err))
			return
		}
		if _, err := s.accounts.Select(a.ID); err != nil {
			s.markSessionSwitchPending()
			s.redirectAdmin(w, r, "", fmt.Sprintf("Chrome 已重启为 %s，但同步当前账号失败: %v", a.Name, err))
			return
		}
		s.sessionSwitchPending.Store(false)
		s.redirectAdmin(w, r, fmt.Sprintf("已选用 %s 并自动重启 Chrome（%s）", a.Name, a.SessionDir), "")
		return
	}
	s.markSessionSwitchPending()
	s.redirectAdmin(w, r, fmt.Sprintf("已选用 %s，请重启 Chrome 以加载 %s", a.Name, a.SessionDir), "")
}

func (s *Server) handleAdminReset(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminAuth(w, r) {
		return
	}
	if s.accounts == nil {
		s.redirectAdmin(w, r, "", "account store not configured")
		return
	}
	id, err := parsePathID(r)
	if err != nil {
		s.redirectAdmin(w, r, "", "invalid id")
		return
	}
	_ = r.ParseForm()
	remaining := account.DefaultFastRemaining
	if v := strings.TrimSpace(r.FormValue("remaining")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			s.redirectAdmin(w, r, "", "invalid remaining")
			return
		}
		remaining = n
	}
	a, err := s.accounts.ResetFast(id, remaining)
	if err != nil {
		s.redirectAdmin(w, r, "", err.Error())
		return
	}
	s.redirectAdmin(w, r, fmt.Sprintf("%s Seedance 剩余已重置为 %d", a.Name, a.FastRemaining), "")
}

func (s *Server) handleAdminDelete(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminAuth(w, r) {
		return
	}
	if s.accounts == nil {
		s.redirectAdmin(w, r, "", "account store not configured")
		return
	}
	id, err := parsePathID(r)
	if err != nil {
		s.redirectAdmin(w, r, "", "invalid id")
		return
	}
	if err := s.accounts.Delete(id); err != nil {
		s.redirectAdmin(w, r, "", err.Error())
		return
	}
	s.redirectAdmin(w, r, "账号已删除", "")
}

func (s *Server) handleAdminUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.checkAdminAuth(w, r) {
		return
	}
	if s.accounts == nil {
		s.redirectAdmin(w, r, "", "account store not configured")
		return
	}
	id, err := parsePathID(r)
	if err != nil {
		s.redirectAdmin(w, r, "", "invalid id")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.redirectAdmin(w, r, "", "invalid form")
		return
	}
	name := r.FormValue("name")
	sessionDir := r.FormValue("session_dir")
	fastStr := r.FormValue("fast_remaining")
	enabledStr := r.FormValue("enabled")

	in := account.UpdateInput{}
	if name != "" {
		in.Name = &name
	}
	if sessionDir != "" {
		in.SessionDir = &sessionDir
	}
	if fastStr != "" {
		n, err := strconv.Atoi(fastStr)
		if err != nil || n < 0 {
			s.redirectAdmin(w, r, "", "invalid fast_remaining")
			return
		}
		in.FastRemaining = &n
	}
	if enabledStr != "" {
		en := enabledStr == "1" || strings.EqualFold(enabledStr, "true") || enabledStr == "on"
		in.Enabled = &en
	}

	a, err := s.accounts.Update(id, in)
	if err != nil {
		s.redirectAdmin(w, r, "", err.Error())
		return
	}
	s.redirectAdmin(w, r, fmt.Sprintf("已更新 %s", a.Name), "")
}

func (s *Server) renderAdmin(w http.ResponseWriter, r *http.Request, flash, errMsg string) {
	data := adminPageData{
		Flash:            flash,
		Error:            errMsg,
		DefaultRemaining: account.DefaultFastRemaining,
	}
	if s.cfg.APIKey != "" {
		// Prefer key from URL/form so redirects keep working; fall back to configured key
		// so Bearer-authenticated page loads still produce working browser forms.
		key := r.URL.Query().Get("key")
		if key == "" {
			key = r.FormValue("key")
		}
		if key == "" {
			key = s.cfg.APIKey
		}
		data.AuthKey = key
	}
	if s.accounts != nil {
		data.ActiveSessionFile = s.accounts.ActiveSessionPath()
		list, err := s.accounts.List()
		if err != nil {
			log.Printf("admin list accounts: %v", err)
			data.Error = err.Error()
		} else {
			data.Accounts = list
		}
		active, err := s.accounts.Active()
		if err == nil {
			data.Active = &active
			data.ActiveID = active.ID
		}
		data.NeedsRestart = s.isSessionSwitchPending()
		if s.workers != nil {
			data.Workers = s.workers.Workers()
		}

		const pageSize = 20
		page := 1
		if v := strings.TrimSpace(r.URL.Query().Get("page")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				page = n
			}
		}
		total, err := s.accounts.CountGenerations()
		if err != nil {
			log.Printf("admin count generations: %v", err)
		}
		totalPages := 1
		if total > 0 {
			totalPages = (total + pageSize - 1) / pageSize
		}
		if page > totalPages {
			page = totalPages
		}
		offset := (page - 1) * pageSize
		gens, err := s.accounts.ListGenerationsPage(offset, pageSize)
		if err != nil {
			log.Printf("admin list generations: %v", err)
		} else {
			if data.AuthKey != "" {
				for i := range gens {
					for j, u := range gens[i].Images {
						gens[i].Images[j] = withAdminKey(u, data.AuthKey)
					}
					gens[i].ResultURL = withAdminKey(gens[i].ResultURL, data.AuthKey)
				}
			}
			data.Generations = gens
		}
		data.HistoryTotal = total
		data.HistoryPage = page
		data.HistoryPageSize = pageSize
		data.HistoryTotalPages = totalPages
		data.HistoryHasPrev = page > 1
		data.HistoryHasNext = page < totalPages
		data.HistoryPrevPage = page - 1
		data.HistoryNextPage = page + 1
		if total == 0 {
			data.HistoryFrom = 0
			data.HistoryTo = 0
		} else {
			data.HistoryFrom = offset + 1
			data.HistoryTo = offset + len(data.Generations)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := adminTmpl.ExecuteTemplate(w, "admin.html", data); err != nil {
		log.Printf("admin render: %v", err)
	}
}

func withAdminKey(raw, key string) string {
	if key == "" || raw == "" {
		return raw
	}
	if !strings.HasPrefix(raw, "/admin/history/media/") {
		return raw
	}
	sep := "?"
	if strings.Contains(raw, "?") {
		sep = "&"
	}
	return raw + sep + "key=" + url.QueryEscape(key)
}

func (s *Server) redirectAdmin(w http.ResponseWriter, r *http.Request, flash, errMsg string) {
	q := ""
	if flash != "" {
		q = "?flash=" + urlQueryEscape(flash)
	}
	if errMsg != "" {
		if q == "" {
			q = "?error=" + urlQueryEscape(errMsg)
		} else {
			q += "&error=" + urlQueryEscape(errMsg)
		}
	}
	// Preserve API key for browser flows when configured.
	if s.cfg.APIKey != "" {
		key := r.URL.Query().Get("key")
		if key == "" {
			key = r.FormValue("key")
		}
		if key == "" && strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		}
		if key != "" {
			if q == "" {
				q = "?key=" + urlQueryEscape(key)
			} else {
				q += "&key=" + urlQueryEscape(key)
			}
		}
	}
	http.Redirect(w, r, "/admin"+q, http.StatusSeeOther)
}

func parsePathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func urlQueryEscape(s string) string {
	return url.QueryEscape(s)
}
