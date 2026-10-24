package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
	"github.com/go-rod/rod"
)

// SnapshotOptions bounds capture independently from the shared fact buffer.
type SnapshotOptions struct {
	MaxNodes    int    `json:"max_nodes"`
	MaxDepth    int    `json:"max_depth"`
	VisibleOnly bool   `json:"visible_only"`
	EmitFacts   bool   `json:"-"`
	CaptureID   string `json:"capture_id"`
}

var snapshotSequence atomic.Uint64

func (o SnapshotOptions) normalized() (SnapshotOptions, error) {
	if o.MaxNodes == 0 {
		o.MaxNodes = 200
	}
	if o.MaxDepth == 0 {
		o.MaxDepth = 12
	}
	if o.MaxNodes < 1 || o.MaxNodes > 500 || o.MaxDepth < 1 || o.MaxDepth > 50 {
		return o, fmt.Errorf("snapshot capture requires max_nodes 1..500 and max_depth 1..50")
	}
	return o, nil
}

// StructuredSnapshot is direct evidence; submitted facts may be evicted at once.
type StructuredSnapshot struct {
	CaptureID        string                   `json:"capture_id"`
	Nodes            []map[string]interface{} `json:"nodes"`
	Scanned          int                      `json:"scanned"`
	DiscoveryScanned int                      `json:"discovery_scanned"`
	Captured         int                      `json:"captured"`
	CaptureComplete  bool                     `json:"capture_complete"`
	Reasons          []string                 `json:"incomplete_reasons"`
	Capability       string                   `json:"capability"`
	FactStorage      map[string]interface{}   `json:"fact_storage"`
}

func (m *SessionManager) captureStructured(ctx context.Context, sessionID string, root *rod.Element, options SnapshotOptions, react bool) (*StructuredSnapshot, error) {
	page, ok := m.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("unknown session: %s", sessionID)
	}
	return m.captureStructuredPage(ctx, sessionID, page, root, options, react)
}

func (m *SessionManager) captureStructuredPage(ctx context.Context, sessionID string, page *rod.Page, root *rod.Element, options SnapshotOptions, react bool) (*StructuredSnapshot, error) {
	options, err := options.normalized()
	if err != nil {
		return nil, err
	}
	options.CaptureID = fmt.Sprintf("%x_%x", time.Now().UnixNano(), snapshotSequence.Add(1))
	script := snapshotScript
	args := rod.Eval(script, options, react).ByPromise()
	var raw []byte
	if root == nil {
		res, evalErr := page.Context(ctx).Evaluate(args)
		if evalErr != nil {
			return nil, fmt.Errorf("structured inspection: %w", evalErr)
		}
		if res == nil {
			return nil, fmt.Errorf("structured inspection returned no evidence")
		}
		raw, err = res.Value.MarshalJSON()
	} else {
		res, evalErr := root.Context(ctx).Evaluate(args)
		if evalErr != nil {
			return nil, fmt.Errorf("structured subtree inspection: %w", evalErr)
		}
		if res == nil {
			return nil, fmt.Errorf("structured subtree inspection returned no evidence")
		}
		raw, err = res.Value.MarshalJSON()
	}
	if err != nil {
		return nil, err
	}
	var result StructuredSnapshot
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode structured inspection: %w", err)
	}
	secrets := snapshotEnvironmentSecrets(m.redactor)
	for i, row := range result.Nodes {
		result.Nodes[i] = cleanSnapshotValue(m.redactor.Sanitize(row), secrets).(map[string]interface{})
	}
	result.Captured = len(result.Nodes)
	result.FactStorage = map[string]interface{}{"requested": options.EmitFacts, "submitted": 0, "retention": "unknown"}
	if !options.EmitFacts {
		result.FactStorage["retention"] = "not_requested"
	}
	if options.EmitFacts {
		if m.engine == nil {
			result.FactStorage["error"] = "mangle engine unavailable"
		} else {
			facts := snapshotFacts(sessionID, result.Nodes, react)
			if err := m.engine.AddFacts(ctx, facts); err != nil {
				result.FactStorage["submitted"] = nil
				result.FactStorage["error"] = cleanSnapshotValue(m.redactor.SanitizeString(err.Error()), secrets)
			} else {
				result.FactStorage["submitted"] = len(facts)
			}
		}
	}
	return &result, nil
}

func snapshotEnvironmentSecrets(redactor *security.Redactor) []string {
	var values []string
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		credentialKey := false
		upper := strings.ToUpper(key)
		for _, marker := range []string{"PASSWORD", "PASSWD", "SECRET", "TOKEN", "API_KEY", "APIKEY", "CREDENTIAL", "PRIVATE_KEY", "AUTHORIZATION"} {
			if strings.Contains(upper, marker) {
				credentialKey = true
				break
			}
		}
		if ok && value != "" && (credentialKey || redactor.RedactInputValue(key, value) == security.Redacted) {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	return values
}

func cleanSnapshotValue(value interface{}, secrets []string) interface{} {
	switch v := value.(type) {
	case string:
		for _, secret := range secrets {
			v = strings.ReplaceAll(v, secret, security.Redacted)
		}
		return v
	case map[string]interface{}:
		clean := make(map[string]interface{}, len(v))
		for key, item := range v {
			clean[cleanSnapshotValue(key, secrets).(string)] = cleanSnapshotValue(item, secrets)
		}
		return clean
	case []interface{}:
		for i, item := range v {
			v[i] = cleanSnapshotValue(item, secrets)
		}
	}
	return value
}

func snapshotFacts(sessionID string, nodes []map[string]interface{}, react bool) []mangle.Fact {
	now := time.Now()
	facts := make([]mangle.Fact, 0, len(nodes)*3)
	add := func(predicate string, args ...interface{}) {
		facts = append(facts, mangle.Fact{Predicate: predicate, Args: append([]interface{}{sessionID}, args...), Timestamp: now})
	}
	for _, n := range nodes {
		if react {
			add("react_component", n["id"], n["name"], n["parent"])
			for key, value := range n["props"].(map[string]interface{}) {
				encoded, _ := json.Marshal(value)
				add("react_prop", n["id"], key, string(encoded))
			}
			for key, value := range n["state"].(map[string]interface{}) {
				encoded, _ := json.Marshal(value)
				add("react_state", n["id"], key, string(encoded))
			}
			if id, _ := n["dom_id"].(string); id != "" {
				add("dom_mapping", n["id"], id)
			}
		} else {
			add("dom_node", n["id"], n["tag"], n["text"], n["parent"])
			if n["text"] != "" {
				add("dom_text", n["id"], n["text"])
			}
			for key, value := range n["attrs"].(map[string]interface{}) {
				add("dom_attr", n["id"], key, value)
			}
			layout := n["layout"].(map[string]interface{})
			add("dom_layout", n["id"], layout["x"], layout["y"], layout["width"], layout["height"], fmt.Sprint(n["visible"]))
		}
	}
	return facts
}

// The caller supplies options only as JSON arguments. No user script is accepted.
const snapshotScript = `function(options, react) {
 const root = this instanceof Element ? this : document.documentElement;
 const reasons = new Set(); const nodes = []; const limit = options.max_nodes;
 const domIDs=new WeakMap();let domSequence=0;const domIdentity=el=>{if(!domIDs.has(el))domIDs.set(el,'dom_'+options.capture_id+'_'+(++domSequence));return domIDs.get(el);};
 const scanLimit = limit * 4; let scanned = 0, discoveryScanned = 0, bytes = 0;
 const sensitive = /password|passwd|secret|token|authorization|cookie|api[-_]?key|credential|one-time-code|cc-number|cc-csc|cvv|cvc/i;
 const text = value => { const s = String(value == null ? '' : value); if(s.length>256){reasons.add('scalar_length_limit');return '[STRING_EXCEEDS_256]';}return s; };
 const own = (obj,key) => {if(!obj || typeof obj!=='object')return undefined;const d=Object.getOwnPropertyDescriptor(obj,key);return d && 'value' in d ? d.value : undefined;};
 const scalar = value => {if(typeof value==='function')return {type:'function'}; if(value===null || typeof value==='boolean' || (typeof value==='number'&&Number.isFinite(value)))return value;if(typeof value==='string')return text(value);return undefined;};
 const visible = el => {if(!(el instanceof Element))return true;const r=el.getBoundingClientRect(),s=getComputedStyle(el);return s.display!=='none'&&s.visibility!=='hidden'&&s.opacity!=='0'&&r.width>0&&r.height>0;};
 const descriptor = el => el instanceof Element ? ['type','autocomplete','id','name','aria-label'].map(k=>el.getAttribute(k)||'').join(' ') : '';
 const append = row => {const size=JSON.stringify(row).length;if(bytes+size>131072){reasons.add('capture_byte_limit');return false;}bytes+=size;nodes.push(row);return true;};
 const safeFields = (obj, hide) => {const result={};if(!obj||typeof obj!=='object')return result;let count=0;for(const key of Object.keys(obj)){if(count++===16){reasons.add('field_limit');break;}const value=scalar(own(obj,key));if(value!==undefined)result[text(key)]=hide||sensitive.test(key)?'[REDACTED]':value;}return result;};
 let capability='available';
 if (!root || !root.isConnected) throw new Error('inspection root is detached');
 if (!react) {
  let el=root, depth=0;
  while(el){
   if(scanned>=scanLimit){reasons.add('scan_limit');break;} scanned++;
   const isVisible=visible(el), hide=sensitive.test(descriptor(el));
   if(!options.visible_only||isVisible){
    const attrs={};let count=0;
    for(const attr of el.attributes){if(!/^(id|role|type|name|value|title|href|disabled|autocomplete|aria-[a-z-]+|data-(state|value|testid))$/.test(attr.name))continue;if(count++===16){reasons.add('field_limit');break;}attrs[text(attr.name)]=hide&&attr.name==='value'||sensitive.test(attr.name)?'[REDACTED]':text(attr.value);}
    let direct='';if(!/^(SCRIPT|STYLE|NOSCRIPT)$/.test(el.tagName)){let n=0;for(const child of el.childNodes){if(n++===32){reasons.add('text_node_limit');break;}if(child.nodeType===Node.TEXT_NODE)direct+=text(child.data);}}
    const rect=el.getBoundingClientRect();
    const row={id:domIdentity(el),dom_id:text(el.id),tag:el.tagName,parent:el===root?'':domIdentity(el.parentElement),depth,attrs,text:hide?'[REDACTED]':text(direct.trim()),visible:isVisible,layout:{x:rect.x,y:rect.y,width:rect.width,height:rect.height}};
    if(/^(INPUT|SELECT|TEXTAREA|OPTION|BUTTON)$/.test(el.tagName))row.value=hide?'[REDACTED]':text(el.value);
    if(!append(row))break;
    if(nodes.length>=limit){if(el.firstElementChild||el!==root&&el.nextElementSibling)reasons.add('node_limit');else{let p=el;while(p!==root){p=p.parentElement;if(p!==root&&p.nextElementSibling){reasons.add('node_limit');break;}}}break;}
   }
   if(el.firstElementChild&&depth<options.max_depth){el=el.firstElementChild;depth++;continue;}
   if(el.firstElementChild)reasons.add('depth_limit');
   while(el!==root&&!el.nextElementSibling){el=el.parentElement;depth--;}
   if(el===root)break;el=el.nextElementSibling;
  }
 } else {
  const fiberAt = el => {for(const key of Object.keys(el)){if(key.startsWith('__reactFiber'))return el[key];if(key.startsWith('__reactContainer')){const value=el[key];return value&&value.current||value;}}return null;};
  let fiber=fiberAt(root);
  if(!fiber){let el=root;while(el&&discoveryScanned<scanLimit){discoveryScanned++;fiber=fiberAt(el);if(fiber)break;if(el.firstElementChild){el=el.firstElementChild;continue;}while(el!==root&&!el.nextElementSibling)el=el.parentElement;if(el===root)break;el=el.nextElementSibling;}if(!fiber){capability='unavailable';reasons.add('no_accessible_react_root_within_scan_budget');}else if(root===document.documentElement){reasons.add('document_first_react_tree');}}
  const stack=fiber?[{fiber,parent:'',depth:0}]:[];const seen=new Set();
  while(stack.length){
   if(scanned>=scanLimit){reasons.add('scan_limit');break;}
   const item=stack.pop(), f=item.fiber;if(!f||seen.has(f))continue;seen.add(f);scanned++;
   const id='fiber_'+options.capture_id+'_'+scanned;const el=f.stateNode instanceof Element?f.stateNode:null;
   const name=text(typeof f.type==='string'?f.type:typeof f.type==='function'?f.type.displayName||f.type.name: f.type&&f.type.displayName||'Anonymous');
   const props=f.memoizedProps;const hide=sensitive.test(name+' '+descriptor(el)+' '+['type','id','name','autoComplete'].map(k=>typeof own(props,k)==='string'?own(props,k):'').join(' '));
   if(!options.visible_only||!el||visible(el)){
    const state={};let hook=f.memoizedState,h=0;
    if(Array.isArray(hook)){for(const v of hook.slice(0,8)){const s=scalar(v);if(s!==undefined)state[h++]=hide?'[REDACTED]':s;}if(hook.length>8)reasons.add('state_limit');}
    else {const visited=new Set();while(hook&&typeof hook==='object'&&!visited.has(hook)&&h<8){visited.add(hook);const s=scalar(own(hook,'memoizedState'));if(s!==undefined)state[h]=hide?'[REDACTED]':s;hook=own(hook,'next');h++;}if(hook&&h===8)reasons.add('state_limit');}
    if(!append({id,name,parent:item.parent,depth:item.depth,dom_id:el?text(el.id):'',props:safeFields(props,hide),state}))break;
    if(nodes.length>=limit){if(stack.length||f.child||item.depth>0&&f.sibling)reasons.add('node_limit');break;}
   }
   if(item.depth>0&&f.sibling)stack.push({fiber:f.sibling,parent:item.parent,depth:item.depth});
   if(f.child){if(item.depth<options.max_depth)stack.push({fiber:f.child,parent:id,depth:item.depth+1});else reasons.add('depth_limit');}
  }
 }
 return {capture_id:options.capture_id,nodes,scanned,discovery_scanned:discoveryScanned,captured:nodes.length,capture_complete:reasons.size===0,incomplete_reasons:Array.from(reasons),capability};
}`
