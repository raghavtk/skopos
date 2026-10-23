package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/raghavtk/skopos/internal/correlate"
	"github.com/raghavtk/skopos/internal/model"
	"github.com/raghavtk/skopos/internal/otlp"
	"github.com/raghavtk/skopos/internal/store"
)

type Server struct {
	Store *store.Store
	Token string
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]interface{}{"status": "ok", "events": len(s.Store.Events())})
	})
	mux.HandleFunc("/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			s.ingest(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		limit := 100
		if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
		all := s.Store.Events()
		out := make([]model.Event, 0)
		for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
			e := all[i]
			if v := r.URL.Query().Get("type"); v != "" && e.Type != v {
				continue
			}
			if v := r.URL.Query().Get("node"); v != "" && e.Node != v {
				continue
			}
			if v := r.URL.Query().Get("request_id"); v != "" && e.RequestID != v {
				continue
			}
			out = append(out, e)
		}
		respond(w, out)
	})
	mux.HandleFunc("/v1/requests/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", 405)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/requests/")
		export := strings.HasSuffix(id, "/otlp")
		if export {
			id = strings.TrimSuffix(id, "/otlp")
		}
		if id == "" || strings.Contains(id, "/") {
			http.Error(w, "invalid request ID", 400)
			return
		}
		result, ok := correlate.Build(s.Store.Events(), id)
		if !ok {
			http.Error(w, "request not found", 404)
			return
		}
		if export {
			respond(w, otlp.Encode(result))
		} else {
			respond(w, result)
		}
	})
	mux.HandleFunc("/v1/topology", func(w http.ResponseWriter, r *http.Request) { respond(w, correlate.Topology(s.Store.Events())) })
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, ui)
	})
	return mux
}

func (s Server) ingest(w http.ResponseWriter, r *http.Request) {
	if s.Token != "" && r.Header.Get("Authorization") != "Bearer "+s.Token {
		http.Error(w, "unauthorized", 401)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	dec := json.NewDecoder(r.Body)
	var batch []model.Event
	if err := dec.Decode(&batch); err != nil {
		http.Error(w, "invalid JSON batch: "+err.Error(), 400)
		return
	}
	if len(batch) > 1000 {
		http.Error(w, "batch exceeds 1000 events", 413)
		return
	}
	for _, e := range batch {
		if e.Version != 1 || e.ID == "" || e.Type == "" || e.Time.IsZero() {
			http.Error(w, "invalid event", 400)
			return
		}
	}
	added, err := s.Store.AddBatch(batch)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	respond(w, map[string]int{"accepted": added, "duplicates": len(batch) - added})
}

func respond(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

const ui = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Skopos</title><style>
body{font:15px system-ui;background:#0b1220;color:#dbe7fb;margin:0}main{max-width:1100px;margin:auto;padding:32px}h1{color:#80e0c4;letter-spacing:.04em}input,button{font:inherit;padding:10px;border-radius:6px;border:1px solid #53657e;background:#172238;color:#fff}button{cursor:pointer;background:#196b61;border-color:#31b6a0}.grid{display:grid;grid-template-columns:1fr 1fr;gap:18px}.card{background:#121e30;border:1px solid #26364e;border-radius:10px;padding:18px;margin:12px 0}.muted{color:#94a9c5}.bad{color:#ff9e88}.good{color:#80e0c4}li{margin:8px 0}code{color:#c4d8ff}@media(max-width:700px){.grid{grid-template-columns:1fr}}
</style></head><body><main><h1>SKOPOS</h1><p class="muted">Request evidence from the network and Kubernetes.</p><form id="lookup"><input id="rid" placeholder="Request ID, e.g. abc123" aria-label="Request ID"><button>Inspect request</button></form><div id="result"></div><h2>Service topology</h2><div id="topology" class="card">Loading…</div></main><script>
const esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
async function topology(){let r=await fetch('/v1/topology'),v=await r.json();document.querySelector('#topology').innerHTML=v.length?v.map(x=>'<div>'+esc(x.from)+' → '+esc(x.to)+' <span class="muted">('+x.requests+' requests, '+x.failures+' failures)</span></div>').join(''):'No observed service calls yet.'}
document.querySelector('#lookup').onsubmit=async e=>{e.preventDefault();let id=document.querySelector('#rid').value.trim();if(!id)return;let el=document.querySelector('#result');let r=await fetch('/v1/requests/'+encodeURIComponent(id));if(!r.ok){el.innerHTML='<div class="card bad">Request not found.</div>';return}let x=await r.json();el.innerHTML='<div class="grid"><div class="card"><h2>'+esc(x.id)+'</h2><p>'+esc(x.services.join(' → '))+'</p><p>'+x.duration_ms.toFixed(1)+' ms · '+esc(x.correlation)+' correlation</p></div><div class="card"><h2>Diagnosis</h2>'+(x.diagnoses.length?x.diagnoses.map(d=>'<p><b>'+esc(d.cause)+'</b> ('+Math.round(d.confidence*100)+'%): '+esc(d.summary)+'</p>').join(''):'No failure evidence.')+'</div></div><div class="card"><h2>Evidence timeline</h2><ol>'+x.evidence.map(v=>'<li><code>'+esc(v.time)+'</code> '+esc(v.summary)+' <span class="muted">['+esc(v.event_id)+']</span></li>').join('')+'</ol></div>'};topology();
</script></body></html>`
