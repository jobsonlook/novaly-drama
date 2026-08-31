package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mask/ai/doubao-web-api/internal/cdp"
	"github.com/mask/ai/doubao-web-api/internal/chrome"
	"github.com/mask/ai/doubao-web-api/internal/config"
)

type inventory struct {
	CreatedAt string             `json:"created_at"`
	Session   string             `json:"session_dir"`
	Sessions  []string           `json:"sessions,omitempty"`
	Videos    []cdp.HistoryVideo `json:"videos"`
	Stats     map[string]int     `json:"stats"`
}

func main() {
	outDir := flag.String("out", "data/unwatermark_staging", "output directory for inventory + mp4s")
	probeID := flag.String("probe", "", "probe a single conversation id (gate check)")
	limit := flag.Int("limit", 0, "max conversations to scan per session (0 = all)")
	sessionDir := flag.String("session", "", "chrome session dir (default: active_session / DOUBAO_SESSION_DIR)")
	allSessions := flag.Bool("all-sessions", false, "scan every account session under ./session/*")
	skipDownload := flag.Bool("skip-download", false, "resolve clean URLs but do not download mp4")
	skipExisting := flag.Bool("skip-existing", false, "skip download when {vid}.mp4 already exists in out dir")
	merge := flag.Bool("merge", false, "merge into existing inventory.json instead of replacing")
	onlyNewConvs := flag.Bool("only-new-convs", false, "with -merge: skip conversation ids already present in inventory")
	sinceStr := flag.String("since", "", "only chats visited on/after this date (YYYY-MM-DD, Asia/Shanghai)")
	untilStr := flag.String("until", "", "only chats visited before this date (YYYY-MM-DD exclusive, Asia/Shanghai)")
	cdpURL := flag.String("cdp", "", "CDP URL override")
	flag.Parse()

	since, until, err := parseDateRange(*sinceStr, *untilStr)
	if err != nil {
		log.Fatalf("date range: %v", err)
	}

	cfg := config.Load()
	if *cdpURL != "" {
		cfg.CDPURL = *cdpURL
	}

	root, _ := os.Getwd()
	out := *outDir
	if !filepath.IsAbs(out) {
		out = filepath.Join(root, out)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatalf("mkdir out: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	browser := cdp.NewBrowser(cfg.CDPURL)
	var mgr *chrome.Manager
	if cfg.AutoRestartChrome {
		mgr = chrome.NewManager(cfg.CDPURL, cfg.CDPPort, cfg.ChromeScript, browser)
		if err := mgr.EnsureStarted(ctx); err != nil {
			log.Fatalf("chrome: %v", err)
		}
	} else if err := browser.Start(ctx); err != nil {
		log.Fatalf("connect chrome: %v (run ./scripts/start-chrome.sh)", err)
	}
	defer browser.Close()

	loggedIn, err := browser.IsLoggedIn(ctx)
	if err != nil {
		log.Printf("warning: login check: %v", err)
	} else if !loggedIn {
		log.Printf("warning: not logged in — open https://www.doubao.com/chat/ in Chrome")
	}

	if *probeID != "" {
		hv, err := browser.ProbeConversation(ctx, *probeID)
		if err != nil {
			log.Fatalf("probe failed: %v", err)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(hv)
		ok := hv.CleanURL != "" && (hv.FallbackAPI != "" || strings.Contains(hv.CleanURL, "unwatermarked"))
		if !ok {
			log.Printf("GATE FAIL: need fallback_api unwatermarked url (got vid=%s clean=%v err=%s)", hv.Vid, hv.CleanURL != "", hv.Error)
			os.Exit(2)
		}
		log.Printf("GATE OK: fallback_api=%v clean_url=%v vid=%s unwatermarked=%v",
			hv.FallbackAPI != "", hv.CleanURL != "", hv.Vid, strings.Contains(hv.CleanURL, "unwatermarked"))
		// Optional download into -out (for targeted re-fetch).
		if !*skipDownload && hv.CleanURL != "" && strings.Contains(strings.ToLower(hv.CleanURL), "unwatermarked") {
			name := hv.Vid
			if name == "" {
				name = "probe_" + *probeID
			}
			dest := filepath.Join(out, name+".mp4")
			if *skipExisting {
				if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
					log.Printf("skip existing %s (%d bytes)", dest, st.Size())
					return
				}
			}
			n, err := cdp.DownloadVideoURL(ctx, hv.CleanURL, dest)
			if err != nil {
				log.Fatalf("download failed: %v", err)
			}
			hv.LocalPath = dest
			hv.Bytes = n
			log.Printf("saved %s (%d bytes)", dest, n)
			if *merge {
				inv, _ := loadInventory(out)
				if inv.Stats == nil {
					inv.Stats = map[string]int{}
				}
				replaced := false
				if hv.Vid != "" {
					for i := range inv.Videos {
						if inv.Videos[i].Vid == hv.Vid {
							inv.Videos[i] = hv
							replaced = true
							break
						}
					}
				}
				if !replaced {
					inv.Videos = append(inv.Videos, hv)
				}
				inv.Stats["downloaded"]++
				if _, err := writeInventory(out, inv); err != nil {
					log.Printf("warn: write inventory: %v", err)
				}
			}
		}
		return
	}

	sessions := resolveSessions(root, cfg, *sessionDir, *allSessions)
	if len(sessions) == 0 {
		log.Fatal("no chrome sessions found")
	}
	log.Printf("sessions to scan: %v", sessions)
	if !since.IsZero() || !until.IsZero() {
		log.Printf("date filter: since=%v until=%v (Chrome last_visit_time)", since, until)
	}

	inv := inventory{
		CreatedAt: time.Now().Format(time.RFC3339),
		Sessions:  sessions,
		Videos:    nil,
		Stats:     map[string]int{},
	}
	seenVid := map[string]struct{}{}
	seenConv := map[string]struct{}{}
	if *merge {
		if prev, err := loadInventory(out); err == nil {
			inv.Videos = prev.Videos
			for _, hv := range prev.Videos {
				if hv.Vid != "" {
					seenVid[hv.Vid] = struct{}{}
				}
				if hv.ConversationID != "" {
					seenConv[hv.ConversationID] = struct{}{}
				}
			}
			log.Printf("merge: loaded %d existing videos (%d convs) from inventory", len(inv.Videos), len(seenConv))
		}
	}

	for _, sess := range sessions {
		rel := sess
		if strings.HasPrefix(sess, root+string(os.PathSeparator)) {
			rel = "./" + strings.TrimPrefix(sess, root+string(os.PathSeparator))
		}
		log.Printf("=== session %s ===", sess)
		if err := switchSession(ctx, mgr, cfg, rel); err != nil {
			log.Printf("switch session %s: %v", sess, err)
			inv.Stats["session_errors"]++
			continue
		}
		inv.Session = sess

		ids, err := cdp.ListConversationIDsFromChromeHistoryBetween(sess, since, until)
		if err != nil {
			log.Printf("list history %s: %v", sess, err)
			inv.Stats["history_errors"]++
			continue
		}
		log.Printf("found %d conversation ids", len(ids))
		if *onlyNewConvs && len(seenConv) > 0 {
			filtered := ids[:0]
			for _, id := range ids {
				if _, ok := seenConv[id]; ok {
					inv.Stats["skip_known_conv"]++
					continue
				}
				filtered = append(filtered, id)
			}
			ids = filtered
			log.Printf("after only-new-convs: %d remaining", len(ids))
		}
		if *limit > 0 && len(ids) > *limit {
			ids = ids[:*limit]
		}

		for i, id := range ids {
			log.Printf("[%d/%d] scan %s", i+1, len(ids), id)
			items, err := browser.ScanConversation(ctx, id, true)
			if err != nil {
				log.Printf("  error: %v", err)
				inv.Stats["errors"]++
				continue
			}
			if len(items) == 0 {
				inv.Stats["empty"]++
				continue
			}
			for _, hv := range items {
				if hv.Vid != "" {
					if _, ok := seenVid[hv.Vid]; ok {
						inv.Stats["dup_vid"]++
						dest := filepath.Join(out, hv.Vid+".mp4")
						if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
							inv.Stats["skip_existing"]++
							continue
						}
						// Known vid but file missing — fall through to re-download.
						log.Printf("  re-download missing file for known vid %s", hv.Vid)
					} else {
						seenVid[hv.Vid] = struct{}{}
					}
				}
				inv.Stats["found"]++
				if hv.FallbackAPI != "" {
					inv.Stats["with_fallback"]++
				}
				if hv.CleanURL != "" {
					inv.Stats["with_clean"]++
				}
				if hv.Error != "" {
					inv.Stats["resolve_errors"]++
				}

				if !*skipDownload && hv.CleanURL != "" {
					if !strings.Contains(strings.ToLower(hv.CleanURL), "unwatermarked") {
						hv.Error = "skip_download: clean_url missing lr=unwatermarked"
						inv.Stats["skip_watermarked"]++
						log.Printf("  skip watermarked url for %s", hv.Vid)
					} else {
						name := hv.Vid
						if name == "" {
							name = fmt.Sprintf("conv_%s_%d", id, len(inv.Videos))
						}
						dest := filepath.Join(out, name+".mp4")
						if *skipExisting {
							if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
								hv.LocalPath = dest
								hv.Bytes = st.Size()
								inv.Stats["skip_existing"]++
								log.Printf("  skip existing %s (%d bytes)", dest, st.Size())
								inv.Videos = append(inv.Videos, hv)
								continue
							}
						}
						n, err := cdp.DownloadVideoURL(ctx, hv.CleanURL, dest)
						if err != nil {
							hv.Error = "download: " + err.Error()
							inv.Stats["download_errors"]++
							log.Printf("  download fail: %v", err)
						} else {
							hv.LocalPath = dest
							hv.Bytes = n
							inv.Stats["downloaded"]++
							log.Printf("  saved %s (%d bytes)", dest, n)
						}
					}
				}
				// Avoid duplicating inventory rows for known vids we just refreshed.
				replaced := false
				if hv.Vid != "" {
					for i := range inv.Videos {
						if inv.Videos[i].Vid == hv.Vid {
							inv.Videos[i] = hv
							replaced = true
							break
						}
					}
				}
				if !replaced {
					inv.Videos = append(inv.Videos, hv)
				}
			}
			_, _ = writeInventory(out, inv)
		}
	}

	path, err := writeInventory(out, inv)
	if err != nil {
		log.Fatalf("write inventory: %v", err)
	}
	log.Printf("done → %s stats=%v videos=%d", path, inv.Stats, len(inv.Videos))
	keptFiles := 0
	for _, hv := range inv.Videos {
		if hv.LocalPath == "" {
			continue
		}
		if st, err := os.Stat(hv.LocalPath); err == nil && st.Size() > 0 {
			keptFiles++
		}
	}
	if inv.Stats["with_fallback"] == 0 && inv.Stats["with_clean"] == 0 && keptFiles == 0 {
		log.Printf("GATE FAIL: no fallback_api in scanned history — do not overwrite Novaly")
		os.Exit(2)
	}
	if inv.Stats["downloaded"] == 0 && inv.Stats["skip_existing"] == 0 && keptFiles == 0 {
		log.Printf("GATE FAIL: no unwatermarked mp4 downloaded — do not overwrite Novaly")
		os.Exit(2)
	}
	log.Printf("GATE OK: fallback=%d clean=%d downloaded=%d skip_existing=%d kept_files=%d",
		inv.Stats["with_fallback"], inv.Stats["with_clean"], inv.Stats["downloaded"], inv.Stats["skip_existing"], keptFiles)
}

func parseDateRange(sinceStr, untilStr string) (since, until time.Time, err error) {
	loc, locErr := time.LoadLocation("Asia/Shanghai")
	if locErr != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	parse := func(s string) (time.Time, error) {
		s = strings.TrimSpace(s)
		if s == "" {
			return time.Time{}, nil
		}
		t, e := time.ParseInLocation("2006-01-02", s, loc)
		if e != nil {
			return time.Time{}, e
		}
		return t, nil
	}
	since, err = parse(sinceStr)
	if err != nil {
		return
	}
	until, err = parse(untilStr)
	return
}

func loadInventory(out string) (inventory, error) {
	path := filepath.Join(out, "inventory.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return inventory{}, err
	}
	var inv inventory
	if err := json.Unmarshal(b, &inv); err != nil {
		return inventory{}, err
	}
	if inv.Stats == nil {
		inv.Stats = map[string]int{}
	}
	return inv, nil
}

func resolveSessions(root string, cfg config.Config, sessionFlag string, all bool) []string {
	if sessionFlag != "" {
		sess := sessionFlag
		if !filepath.IsAbs(sess) {
			sess = filepath.Join(root, sess)
		}
		return []string{sess}
	}
	if all {
		base := filepath.Join(root, "session")
		dirs := cdp.DiscoverSessionDirs(base)
		if len(dirs) > 0 {
			return dirs
		}
	}
	sess := ""
	if active := strings.TrimSpace(readFile(cfg.ActiveSession)); active != "" {
		sess = active
	} else if v := strings.TrimSpace(os.Getenv("DOUBAO_SESSION_DIR")); v != "" {
		sess = v
	} else {
		sess = "./session"
	}
	if !filepath.IsAbs(sess) {
		sess = filepath.Join(root, sess)
	}
	return []string{sess}
}

func switchSession(ctx context.Context, mgr *chrome.Manager, cfg config.Config, sessionRel string) error {
	activePath := cfg.ActiveSession
	if !filepath.IsAbs(activePath) {
		wd, _ := os.Getwd()
		activePath = filepath.Join(wd, activePath)
	}
	if err := os.MkdirAll(filepath.Dir(activePath), 0o755); err != nil {
		return err
	}
	cur := strings.TrimSpace(readFile(activePath))
	if cur == sessionRel || filepath.Clean(cur) == filepath.Clean(sessionRel) {
		return nil
	}
	if err := os.WriteFile(activePath, []byte(sessionRel+"\n"), 0o644); err != nil {
		return err
	}
	log.Printf("wrote active_session → %s", sessionRel)
	if mgr == nil {
		log.Printf("chrome auto-restart disabled; restart Chrome manually for session switch")
		return nil
	}
	return mgr.Restart(ctx)
}

func writeInventory(out string, inv inventory) (string, error) {
	path := filepath.Join(out, "inventory.json")
	b, err := json.MarshalIndent(inv, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o644)
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
