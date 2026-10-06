// Package api serves the blockchain and the rental ledger as JSON under /api.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"

	"car-rent-blockchain/blockchain"
	"car-rent-blockchain/rental"
)

// Server holds one in-memory chain and the live state. The mutex lives here, not in the
// blockchain package: handlers run concurrently, and mining plus appending must not.
type Server struct {
	mu        sync.RWMutex
	bc        *blockchain.Blockchain
	state     *rental.State
	debug     bool
	originals map[int]string // Data of blocks changed by the tamper demo
	index     http.Handler
}

// New creates a server with a fresh chain. index is the page served at "/" (may be nil).
func New(difficulty int, debug bool, index http.Handler) *Server {
	return &Server{
		bc:        blockchain.NewBlockchain(difficulty),
		state:     rental.NewState(),
		debug:     debug,
		originals: map[int]string{},
		index:     index,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/config", s.getConfig)
	mux.HandleFunc("GET /api/blocks", s.getBlocks)
	mux.HandleFunc("GET /api/blocks/{index}", s.getBlock)
	mux.HandleFunc("GET /api/validate", s.getValidate)
	mux.HandleFunc("GET /api/accounts", s.getAccounts)
	mux.HandleFunc("GET /api/cars", s.getCars)
	mux.HandleFunc("GET /api/rentals", s.getRentals)
	mux.HandleFunc("POST /api/tx", s.postTx)
	mux.HandleFunc("POST /api/debug/tamper/{index}", s.debugOnly(s.postTamper))
	mux.HandleFunc("POST /api/debug/restore/{index}", s.debugOnly(s.postRestore))
	if s.index != nil {
		mux.Handle("GET /{$}", s.index)
	}
	return mux
}

// ---- JSON helpers

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type errorBody struct {
	Code    string `json:"codigo"`
	Message string `json:"mensagem"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Code: code, Message: message})
}

// ---- blocks

// BlockView is a block as the explorer sees it: the stored fields, the recomputed hash
// and, when the data is a transaction, a pt-BR summary of it.
type BlockView struct {
	Index          int    `json:"index"`
	Timestamp      int64  `json:"timestamp"`
	Nonce          int    `json:"nonce"`
	Hash           string `json:"hash"`
	PreviousHash   string `json:"previousHash"`
	Data           string `json:"data"`
	RecomputedHash string `json:"recomputedHash"`
	HashMatches    bool   `json:"hashMatches"`
	Summary        string `json:"summary"`
	Tampered       bool   `json:"tampered"` // only ever true with --debug
}

func (s *Server) blockView(b *blockchain.Block) BlockView {
	re := blockchain.CalculateHash(b)
	summary := ""
	if b.Index > 0 {
		summary = rental.Summarize(b.Data)
	}
	_, tampered := s.originals[b.Index]
	return BlockView{
		Index: b.Index, Timestamp: b.Timestamp, Nonce: b.Nonce, Hash: b.Hash,
		PreviousHash: b.PreviousHash, Data: b.Data, RecomputedHash: re,
		HashMatches: re == b.Hash, Summary: summary, Tampered: tampered,
	}
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"difficulty": s.bc.Difficulty, "debug": s.debug})
}

func (s *Server) getBlocks(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	views := make([]BlockView, len(s.bc.Blocks))
	for i, b := range s.bc.Blocks {
		views[i] = s.blockView(b)
	}
	writeJSON(w, http.StatusOK, map[string]any{"difficulty": s.bc.Difficulty, "blocks": views})
}

func (s *Server) getBlock(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.blockAt(r.PathValue("index"))
	if !ok {
		writeError(w, http.StatusNotFound, "ErrBlockNotFound", "Bloco não encontrado.")
		return
	}
	writeJSON(w, http.StatusOK, s.blockView(b))
}

func (s *Server) blockAt(raw string) (*blockchain.Block, bool) {
	i, err := strconv.Atoi(raw)
	if err != nil || i < 0 || i >= len(s.bc.Blocks) {
		return nil, false
	}
	return s.bc.Blocks[i], true
}

func (s *Server) getValidate(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, http.StatusOK, rental.ValidateChain(s.bc))
}

// ---- state

type accountView struct {
	Name      string      `json:"name"`
	Role      rental.Role `json:"role"`
	Balance   uint64      `json:"balance"`
	NextNonce uint64      `json:"nextNonce"`
}

func (s *Server) getAccounts(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.state
	accounts := []accountView{}
	for _, name := range st.AccountNames() {
		accounts = append(accounts, accountView{name, st.Roles[name], st.Balances[name], st.Nonces[name] + 1})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts": accounts,
		"escrow":   st.Balances[rental.Escrow],
		"minted":   st.Minted,
	})
}

func (s *Server) getCars(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"cars": s.state.CarList()})
}

func (s *Server) getRentals(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"rentals": s.state.RentalList()})
}

// ---- transactions

func (s *Server) postTx(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, 1<<16)
	var tx rental.Tx
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&tx); err != nil {
		writeError(w, http.StatusBadRequest, "ErrBadArgs", rental.Message(rental.ErrBadArgs))
		return
	}
	if len(tx.Args) > 0 {
		var compact bytes.Buffer
		if err := json.Compact(&compact, tx.Args); err != nil {
			writeError(w, http.StatusBadRequest, "ErrBadArgs", rental.Message(rental.ErrBadArgs))
			return
		}
		tx.Args = compact.Bytes()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if v := rental.ValidateChain(s.bc); !v.Valid {
		writeError(w, http.StatusConflict, "ErrChainInvalid",
			"A cadeia está inválida. Restaure o bloco adulterado antes de enviar transações.")
		return
	}
	receipt, err := s.state.Apply(&tx)
	if err != nil {
		var re *rental.Error
		code := "ErrInvalidTx"
		if errors.As(err, &re) {
			code = re.Code
		}
		writeError(w, http.StatusUnprocessableEntity, code, rental.Message(err))
		return
	}
	data, err := tx.Encode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ErrInternal", "Erro interno.")
		return
	}
	block := s.bc.NewBlock(data)
	s.bc.AddBlock(block)
	writeJSON(w, http.StatusOK, map[string]any{"block": s.blockView(block), "receipt": receipt})
}

// ---- tamper demo (--debug only)

func (s *Server) debugOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.debug {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}
}

// postTamper replaces a block's Data without re-mining it, to show that the chain notices.
func (s *Server) postTamper(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Data string `json:"data"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "ErrBadArgs", rental.Message(rental.ErrBadArgs))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.blockAt(r.PathValue("index"))
	if !ok {
		writeError(w, http.StatusNotFound, "ErrBlockNotFound", "Bloco não encontrado.")
		return
	}
	if _, saved := s.originals[b.Index]; !saved {
		s.originals[b.Index] = b.Data
	}
	b.Data = body.Data
	writeJSON(w, http.StatusOK, s.blockView(b))
}

func (s *Server) postRestore(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.blockAt(r.PathValue("index"))
	if !ok {
		writeError(w, http.StatusNotFound, "ErrBlockNotFound", "Bloco não encontrado.")
		return
	}
	if orig, saved := s.originals[b.Index]; saved {
		b.Data = orig
		delete(s.originals, b.Index)
	}
	writeJSON(w, http.StatusOK, s.blockView(b))
}
