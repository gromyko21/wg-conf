package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/skip2/go-qrcode"

	"github.com/user/wg-conf/internal/config"
	"github.com/user/wg-conf/internal/peer"
	"github.com/user/wg-conf/internal/store"
)

type Server struct {
	params  *config.ServerParams
	peers   *peer.Service
	store   *store.Store
	apiKey  string
}

func New(params *config.ServerParams, peers *peer.Service, st *store.Store, apiKey string) *Server {
	return &Server{params: params, peers: peers, store: st, apiKey: apiKey}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", s.handleHealth)

	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Get("/server", s.handleServer)
		r.Get("/peers", s.handleListPeers)
		r.Post("/peers", s.handleCreatePeer)
		r.Get("/peers/{name}/config", s.handlePeerConfig)
		r.Get("/peers/{name}/qr", s.handlePeerQR)
		r.Delete("/peers/{name}", s.handleRevokePeer)
		r.Get("/audit", s.handleAudit)
		r.Get("/stats", s.handleStats)
	})

	return r
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.apiKey == "" {
			next.ServeHTTP(w, r)
			return
		}

		key := r.Header.Get("X-API-Key")
		if key == "" {
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				key = strings.TrimPrefix(auth, "Bearer ")
			}
		}
		if key != s.apiKey {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleServer(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"interface":  s.params.ServerWGNIC,
		"endpoint":   s.params.Endpoint(),
		"server_ipv4": s.params.ServerWGIPv4,
		"server_ipv6": s.params.ServerWGIPv6,
		"port":       s.params.ServerPort,
		"allowed_ips": s.params.AllowedIPs,
		"dns":        []string{s.params.ClientDNS1, s.params.ClientDNS2},
	})
}

func (s *Server) handleListPeers(w http.ResponseWriter, r *http.Request) {
	peers, err := s.peers.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, peers)
}

func (s *Server) handleCreatePeer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	result, err := s.peers.Create(r.Context(), req.Name, actorFromRequest(r))
	if err != nil {
		slog.Error("create peer", "name", req.Name, "error", err)
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, peer.ErrInvalidName):
			status = http.StatusBadRequest
		case errors.Is(err, peer.ErrPeerExists):
			status = http.StatusConflict
		case errors.Is(err, peer.ErrNoFreeIP):
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handlePeerConfig(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cfg, err := s.peers.GetConfig(r.Context(), name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, peer.ErrPeerNotFound) || errors.Is(err, peer.ErrConfigMissing) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+name+".conf")
	_, _ = w.Write([]byte(cfg))
}

func (s *Server) handlePeerQR(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cfg, err := s.peers.GetConfig(r.Context(), name)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, peer.ErrPeerNotFound) || errors.Is(err, peer.ErrConfigMissing) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	png, err := qrcode.Encode(cfg, qrcode.Medium, 256)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png)
}

func (s *Server) handleRevokePeer(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := s.peers.Revoke(r.Context(), name, actorFromRequest(r)); err != nil {
		slog.Error("revoke peer", "name", name, "error", err)
		status := http.StatusInternalServerError
		if errors.Is(err, peer.ErrPeerNotFound) || errors.Is(err, peer.ErrConfigMissing) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	entries, err := s.store.ListAudit(r.Context(), 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	usage, err := s.store.LatestUsageByPeer(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var totalRx, totalTx int64
	online := 0
	for _, u := range usage {
		totalRx += u.RxBytes
		totalTx += u.TxBytes
		if u.Online {
			online++
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total_rx_bytes": totalRx,
		"total_tx_bytes": totalTx,
		"online_peers":   online,
		"tracked_peers":  len(usage),
		"by_peer":        usage,
	})
}

func actorFromRequest(r *http.Request) string {
	if actor := r.Header.Get("X-Actor"); actor != "" {
		return actor
	}
	return "api"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
