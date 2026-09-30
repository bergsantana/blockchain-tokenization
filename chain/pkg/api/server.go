// Package api exposes the node over plain HTTP and JSON.
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"

	"carrental/pkg/chain"
	"carrental/pkg/contract"
	"carrental/pkg/crypto"
)

// TxResponse is what POST /tx returns once the transaction is in a block.
// The single-validator chain seals a block per transaction, so a block
// number here already means the transaction is final.
type TxResponse struct {
	TxHash  crypto.Hash       `json:"txHash"`
	Block   uint64            `json:"block"`
	Receipt *contract.Receipt `json:"receipt"`
}

type assetView struct {
	ID uint64 `json:"id"`
	*contract.Asset
	Holder string `json:"holder"` // current token holder, which may be "escrow"
}

type rentalView struct {
	ID uint64 `json:"id"`
	*contract.Rental
}

type Server struct {
	chain *chain.Chain
	mux   *http.ServeMux
}

func NewServer(c *chain.Chain) http.Handler {
	s := &Server{chain: c, mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /tx", s.submitTx)
	s.mux.HandleFunc("GET /assets", s.listAssets)
	s.mux.HandleFunc("GET /assets/{id}", s.getAsset)
	s.mux.HandleFunc("GET /balance/{token}/{addr}", s.getBalance)
	s.mux.HandleFunc("GET /rentals", s.listRentals)
	s.mux.HandleFunc("GET /rentals/{id}", s.getRental)
	s.mux.HandleFunc("GET /roles/{addr}", s.getRoles)
	s.mux.HandleFunc("GET /blocks", s.listBlocks)
	return s
}

// ServeHTTP lets the web app on another port call the node (CORS).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) submitTx(w http.ResponseWriter, r *http.Request) {
	var tx chain.Tx
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&tx); err != nil {
		writeError(w, http.StatusBadRequest, chain.ErrMalformedTx.Error())
		return
	}
	b, err := s.chain.Submit(tx)
	switch {
	case errors.Is(err, chain.ErrMalformedTx), errors.Is(err, chain.ErrInvalidSignature):
		log.Printf("refused %s from %.12s: %v", tx.Method, tx.From, err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	case errors.Is(err, chain.ErrDuplicateTx):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		log.Printf("cannot append block: %v", err)
		writeError(w, http.StatusInternalServerError, "cannot append block")
		return
	}
	rec := b.Receipts[0]
	outcome := "ok"
	if rec.Error != "" {
		outcome = "rejected: " + rec.Error
	}
	log.Printf("block #%d %s from %.12s: %s", b.Index, tx.Method, tx.From, outcome)
	writeJSON(w, http.StatusOK, TxResponse{TxHash: b.Txs[0].Hash, Block: b.Index, Receipt: rec})
}

func (s *Server) listAssets(w http.ResponseWriter, r *http.Request) {
	out := []assetView{}
	s.chain.Read(func(st *contract.State) {
		for _, id := range sortedKeys(st.Assets) {
			out = append(out, assetView{id, st.Assets[id], st.Holder(id)})
		}
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getAsset(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	var view *assetView
	s.chain.Read(func(st *contract.State) {
		if a := st.Assets[id]; err == nil && a != nil {
			view = &assetView{id, a, st.Holder(id)}
		}
	})
	if view == nil {
		writeError(w, http.StatusNotFound, "asset not found")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) getBalance(w http.ResponseWriter, r *http.Request) {
	token, err := strconv.ParseUint(r.PathValue("token"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid token id")
		return
	}
	var bal uint64
	s.chain.Read(func(st *contract.State) { bal = st.Balance(token, r.PathValue("addr")) })
	writeJSON(w, http.StatusOK, bal)
}

func (s *Server) listRentals(w http.ResponseWriter, r *http.Request) {
	out := []rentalView{}
	s.chain.Read(func(st *contract.State) {
		for _, id := range sortedKeys(st.Rentals) {
			out = append(out, rentalView{id, st.Rentals[id]})
		}
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getRental(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	var view *rentalView
	s.chain.Read(func(st *contract.State) {
		if rt := st.Rentals[id]; err == nil && rt != nil {
			view = &rentalView{id, rt}
		}
	})
	if view == nil {
		writeError(w, http.StatusNotFound, "rental not found")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) getRoles(w http.ResponseWriter, r *http.Request) {
	var roles []string
	s.chain.Read(func(st *contract.State) { roles = st.RolesOf(r.PathValue("addr")) })
	writeJSON(w, http.StatusOK, roles)
}

func (s *Server) listBlocks(w http.ResponseWriter, r *http.Request) {
	count, err := strconv.Atoi(r.URL.Query().Get("count"))
	if err != nil || count < 1 {
		count = 10
	}
	writeJSON(w, http.StatusOK, s.chain.Blocks(min(count, 100)))
}

func sortedKeys[V any](m map[uint64]V) []uint64 {
	keys := make([]uint64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // show args exactly as they were signed
	if err := enc.Encode(v); err != nil {
		log.Printf("writing response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
