package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/mattn/go-sqlite3"
)

const (
	feedSize          = 20
	maxPostBodyLength = 500
	maxRequestBody    = 16 << 10 // same 16 KB limit as the reference's express.json()
)

const postSelect = `
  SELECT p.id, p.body, p.created_at, u.username,
         (SELECT count(*) FROM likes l WHERE l.post_id = p.id) AS like_count
    FROM posts p
    JOIN users u ON u.id = p.user_id`

type app struct {
	db        *sql.DB
	jwtSecret []byte
	started   time.Time
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.health)
	mux.HandleFunc("GET /feed", a.feed)
	mux.HandleFunc("GET /posts/{id}", a.getPost)
	mux.HandleFunc("POST /posts", a.requireAuth(a.createPost))
	mux.HandleFunc("POST /posts/{id}/like", a.requireAuth(a.likePost))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return mux
}

// Field order here is the wire order.
type post struct {
	ID        int64  `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"` // stored as TEXT in the wire format; sent as is
	Author    string `json:"author"`
	LikeCount int32  `json:"like_count"`
}

type scanner interface{ Scan(dest ...any) error }

func scanPost(row scanner) (post, error) {
	var p post
	err := row.Scan(&p.ID, &p.Body, &p.CreatedAt, &p.Author, &p.LikeCount)
	return p, err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[error] encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func internalError(w http.ResponseWriter, err error) {
	log.Printf("[error] %v", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

// parseID accepts a positive integer path segment.
func parseID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// GET /health — liveness plus a cheap DB round-trip.
func (a *app) health(w http.ResponseWriter, r *http.Request) {
	if _, err := a.db.ExecContext(r.Context(), "SELECT 1"); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, struct {
			Status string `json:"status"`
			DB     string `json:"db"`
			Error  string `json:"error"`
		}{"degraded", "unreachable", err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Status  string `json:"status"`
		DB      string `json:"db"`
		UptimeS int64  `json:"uptime_s"`
	}{"ok", "ok", int64(math.Round(time.Since(a.started).Seconds()))})
}

// GET /feed — 20 most recent posts with author and like count.
func (a *app) feed(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), postSelect+`
     ORDER BY p.created_at DESC, p.id DESC
     LIMIT ?`, feedSize)
	if err != nil {
		internalError(w, err)
		return
	}
	defer rows.Close()
	posts := make([]post, 0, feedSize)
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			internalError(w, err)
			return
		}
		posts = append(posts, p)
	}
	if err := rows.Err(); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string][]post{"posts": posts})
}

// GET /posts/{id} — one post with author and like count.
func (a *app) getPost(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid post id")
		return
	}
	p, err := scanPost(a.db.QueryRowContext(r.Context(), postSelect+` WHERE p.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "post not found")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]post{"post": p})
}

// POST /posts — create a post as the authenticated user. Body: {"body": "..."}
func (a *app) createPost(w http.ResponseWriter, r *http.Request, u user) {
	var req struct {
		Body any `json:"body"`
	}
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody)).Decode(&req)
	if err != nil && !errors.Is(err, io.EOF) { // an empty body is "body is required", as in Express
		writeError(w, http.StatusBadRequest, "malformed JSON body")
		return
	}
	body, _ := req.Body.(string)
	body = strings.TrimSpace(body)
	if body == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}
	// Count UTF-16 code units, like JavaScript's String#length in the reference.
	if len(utf16.Encode([]rune(body))) > maxPostBodyLength {
		writeError(w, http.StatusBadRequest, "body must be at most "+strconv.Itoa(maxPostBodyLength)+" characters")
		return
	}

	p := post{Body: body, Author: u.username}
	err = a.db.QueryRowContext(r.Context(),
		`INSERT INTO posts (user_id, body) VALUES (?, ?) RETURNING id, created_at`,
		u.id, body).Scan(&p.ID, &p.CreatedAt)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]post{"post": p})
}

// POST /posts/{id}/like — idempotent like: 201 the first time, 200 on repeats.
func (a *app) likePost(w http.ResponseWriter, r *http.Request, u user) {
	postID, ok := parseID(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid post id")
		return
	}
	res, err := a.db.ExecContext(r.Context(),
		`INSERT INTO likes (user_id, post_id) VALUES (?, ?)
     ON CONFLICT (user_id, post_id) DO NOTHING`,
		u.id, postID)
	if err != nil {
		// SQLITE_CONSTRAINT_FOREIGNKEY (787): the post (or user) doesn't exist.
		var sqErr sqlite3.Error
		if errors.As(err, &sqErr) && sqErr.ExtendedCode == sqlite3.ErrConstraintForeignKey {
			writeError(w, http.StatusNotFound, "post not found")
			return
		}
		internalError(w, err)
		return
	}
	affected, err := res.RowsAffected()
	if err != nil {
		internalError(w, err)
		return
	}
	inserted := affected > 0
	status := http.StatusOK
	if inserted {
		status = http.StatusCreated
	}
	writeJSON(w, status, struct {
		Liked        bool  `json:"liked"`
		AlreadyLiked bool  `json:"already_liked"`
		PostID       int64 `json:"post_id"`
	}{true, !inserted, postID})
}
