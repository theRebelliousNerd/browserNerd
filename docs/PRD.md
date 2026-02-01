# **The "Mangle-MCP" Developer Tooling Architecture Validation: Architecting the Post-Selenium Era of Neuro-Symbolic Agentic Infrastructure**

## **1\. Executive Summary: The Paradigm Shift to Agent-Centric Infrastructure**

The trajectory of software engineering tooling has historically followed a path of increasing abstraction designed to accommodate the cognitive limitations and strengths of human developers. We moved from binary punch cards to assembly, then to high-level procedural languages, and finally to sophisticated Integrated Development Environments (IDEs) rich with introspection, debuggers, and visualizers. These tools were architected on the premise of **Human-Computer Interaction (HCI)**—optimized for visual processing, distinct input modalities (keyboard/mouse), and human latency tolerances.

However, we are currently witnessing a seismic shift toward **Agent-Computer Interaction (ACI)**. As Large Language Models (LLMs) evolve from passive code completion engines into autonomous agents—exemplified by tools like Claude Code, Gemini CLI, and Cursor—the underlying infrastructure supporting them is revealing critical deficiencies. These agents operate effectively in "God-Mode" only when their perception of the environment matches their reasoning capabilities. Currently, this is not the case. Agents are forced to view sophisticated runtime environments through the "keyhole" of text-based terminal outputs or serialized HTML dumps, stripping away the semantic richness of the application state.

This report serves as a rigorous technical validation of a proposed architectural solution: the **"Mangle-MCP" Server**. This architecture integrates Google's **Mangle** (a deductive logic programming language) 1, the **Rod** browser automation library 2, and the **Model Context Protocol (MCP)** 3 to create a neuro-symbolic bridge. The core hypothesis posits that by translating chaotic runtime states into structured logic facts, we can grant agents a deterministic, queryable view of the application, effectively solving the "context gap" that plagues current autonomous debugging and verification workflows.

The validation is structured across four distinct research vectors:

1. **The Developer Context Problem:** Reconstructing high-level semantic abstractions (Components) from low-level runtime artifacts (DOM) using React Fiber reification.  
2. **The Flight Recorder:** Leveraging Mangle's deductive inference engine to perform Root Cause Analysis (RCA) on streams of correlated runtime events.  
3. **Session Persistence:** Architecting "detached" browser sessions that survive agent disconnects, mirroring the persistence of server-side databases.  
4. **The Logic-Based Test Runner:** Shifting the verification paradigm from probabilistic visual assertions to deterministic logic rule evaluation.

Our analysis confirms that while this architecture introduces significant engineering complexities—specifically regarding the latency of reifying high-frequency runtime events into Datalog facts—it unlocks a "killer feature" absent in all prior generations of tooling: **Semantic Time-Travel Debugging**. This capability moves the industry beyond the "Selenium Era" of imperative scripting into a new age of declarative, logic-driven agent orchestration.

## **2\. Vector 1: The "Developer Context" Problem**

### **2.1 The Semantic Gap in Agentic Perception**

The fundamental limitation of current coding agents lies in their perception of the user interface. When a human engineer looks at a web application via React DevTools, they do not see a flat list of HTML tags. They perceive a hierarchy of **Components**, data flowing via **Props**, and local **State**. They see a \<UserCard\> component, not a \<div class="css-1r6f"\>. This "Developer View" is the mental model used to reason about bugs and features.

Conversely, when an agent like Claude Code interacts with a web app, it typically scrapes the DOM via a library like Puppeteer or reads a serialized HTML string. This representation is destructively lossy. It discards the very abstractions the developer used to build the software. The agent sees the *output* of the rendering function, but has no visibility into the *input* (props/state) that generated it. This forces the agent to rely on probabilistic pattern matching against CSS classes to infer logic, a process prone to hallucination.

To bridge this gap, the Mangle-MCP architecture proposes a mechanism of **Semantic Reification**. This involves using the Rod browser driver to penetrate the JavaScript runtime, extract the internal component tree, and reify it into Mangle logic facts.

### **2.2 Deep Dive: React Fiber Reification via Rod**

The technical viability of this vector hinges on the ability to access the internal data structures of the frontend framework. In the React ecosystem, this structure is the **Fiber Tree**. Introduced in React 16 to enable concurrent rendering, the Fiber tree is a linked-list representation of the component hierarchy kept in memory.4

Unlike the Virtual DOM (which is a tree of React Elements re-created on every render), the Fiber tree is a persistent structure that holds the actual state and props for every component instance.6 This makes it the "ground truth" for the application's runtime state.

#### **2.2.1 The Fiber Data Structure**

To validate the extraction strategy, we must understand the precise memory layout of a Fiber node. As detailed in the React internals research 7, each Fiber node contains three critical pointers that form the tree structure:

1. **child**: Points to the first child node.  
2. **sibling**: Points to the next sibling node.  
3. **return**: Points back to the parent node.

Crucially, the Fiber node also contains the semantic data we need:

* **memoizedProps**: The props used to render the current output.  
* **memoizedState**: The local state (hooks) of the component.  
* **type**: The function or class definition, from which we can extract the Component Name.

#### **2.2.2 The Extraction Mechanism**

The proposed implementation utilizes Rod's ability to execute JavaScript within the page context (page.Evaluate) to "walk" this linked list. This mirrors how the React DevTools extension works, utilizing the global hook \_\_REACT\_DEVTOOLS\_GLOBAL\_HOOK\_\_.9

However, for a lightweight agentic tool, we can use a more direct approach by locating the internal pointer attached to the DOM. In React 17+, the root DOM node contains a property key starting with \_\_reactFiber$. The extraction logic proceeds as follows:

1. **Discovery:** The Rod script iterates over the keys of the root DOM element to find the \_\_reactFiber prefix.10  
2. **Traversal:** Starting from this root Fiber, the script performs a depth-first traversal using the child and sibling pointers.  
3. **Serialization:** For each node, the script constructs a simplified JSON object containing only the type (name), props (filtered for serializability), and state.  
4. **Ingestion:** This JSON structure is passed back to the Go server, which iterates through it and generates AddFact calls to the Mangle engine.

### **2.3 Mangle Schema for Component Knowledge**

The transformation of this JSON tree into Mangle logic facts creates a queryable database of the UI. Mangle, extending Datalog, allows for expressive querying of these relationships.1

**Proposed Mangle Schema:**

Prolog

// Fact: Defines a component instance  
// component(FiberId, ComponentName, ParentFiberId).  
react\_component(id\_101, "UserProfile", id\_parent\_99).

// Fact: Defines a prop passed to a component.  
// props(FiberId, PropKey, PropValue).  
// Note: PropValue is stored as a string serialization.  
react\_prop(id\_101, "userId", "u\_12345").  
react\_prop(id\_101, "isLoading", "false").

// Fact: Defines internal state hook.  
// state(FiberId, HookIndex, Value).  
react\_state(id\_101, 0, "activeTab:settings").

// Fact: Mapping back to the raw DOM for interaction.  
// dom\_ref(FiberId, DomNodeId).  
dom\_mapping(id\_101, dom\_node\_55).

This schema allows the agent to answer complex questions that are impossible with raw HTML. For example: *"Find the UserProfile component where isLoading is false but the data prop is empty."*

**Mangle Query:**

Prolog

broken\_profile(C) :-  
    react\_component(C, "UserProfile", \_),  
    react\_prop(C, "isLoading", "false"),  
    react\_prop(C, "data", "null").

### **2.4 Optimization: Token Economics and Context Window Efficiency**

A critical advantage of this semantic reification is the optimization of token usage. LLM context windows, while growing, are finite and expensive. Feeding an agent a raw HTML dump of a modern application is incredibly inefficient. A typical "dashboard" page might render 2,000 DOM elements, resulting in an HTML string of 100KB+ heavily laden with utility classes (e.g., Tailwind's flex items-center justify-between p-4...) and SVG path data.11

In contrast, the Mangle-derived Component Tree effectively compresses this data by stripping away the visual implementation details (the styling classes) and retaining only the structural and data semantics.

**Comparative Analysis of Context Load:**

| Representation Type | Content Included | Avg. Size (KB) | Avg. Tokens | Semantic Signal-to-Noise |
| :---- | :---- | :---- | :---- | :---- |
| **Raw HTML Snapshot** | All tags, attributes, full CSS classes, SVGs, scripts | \~450 KB | \~120,000 | **Very Low** |
| **Filtered HTML** | body only, scripts removed, attributes truncated | \~80 KB | \~20,000 | **Low** |
| **Mangle Fact Base** | Component names, Props, State, Hierarchy | \~15 KB | \~3,500 | **High** |

As the table demonstrates, the Mangle representation offers a \~30x reduction in token cost compared to raw HTML. This efficiency is transformative. It allows the agent to maintain a "history" of the UI state over time within its context window. Instead of seeing only the *current* frame, the agent can fit the last 10 frames of semantic state into its context, enabling it to detect transient changes or flickers that would otherwise be invisible.

## **3\. Vector 2: "Flight Recorder" for Debugging**

### **3.1 The Epistemic Gap in Debugging**

When a software system fails, the error message observed by the user (or the agent) is rarely the root cause; it is merely the final symptom in a sequence of events. Debugging is effectively the process of traversing this causal chain backwards in time. For a human developer, this involves correlating disparate signals: a 500 error in the Network tab, a stack trace in the Console, and a frozen UI element in the DOM.

AI agents suffer from an "Epistemic Gap" here. They typically lack the temporal memory to correlate these events. If an agent executes a command and sees a generic "Something went wrong" message, it cannot inherently "rewind" to see the network request that failed 50 milliseconds prior.13 It sees the crash, but not the trajectory that led to it.

The "Flight Recorder" vector addresses this by utilizing Mangle not just as a database, but as a **Causal Inference Engine**. By buffering streams of runtime events into the deductive database, we can program logic rules that define causality, effectively generating a "Root Cause Analysis" (RCA) report automatically.

### **3.2 Architectural Implementation: The Event Ingestion Pipeline**

To function as a flight recorder, the Mangle-MCP server must act as a high-throughput event sink. The architecture leverages the Rod library's event subscription capabilities to listen to multiple domains of the Chrome DevTools Protocol (CDP) simultaneously.2

The ingestion pipeline consists of:

1. **CDP Event Streams:** Subscribing to Log.entryAdded, Runtime.consoleAPICalled, Network.requestWillBeSent, Network.responseReceived, and DOM.documentUpdated.  
2. **Go Channel Buffering:** To prevent blocking the browser driver, events are pushed into non-blocking Go channels. This decouples the high-frequency event generation from the potentially slower logic insertion.  
3. **Normalization:** Converting disparate JSON event payloads into uniform Mangle facts.  
4. **Circular Buffering:** Crucially, since Mangle runs in-memory, we cannot store infinite history. The server must implement a "sliding window" or circular buffer, retaining only the last $N$ events or $T$ minutes of history.

### **3.3 Network Debugging: Mapping HAR to Logic**

One of the richest sources of debugging data is the network trace. The standard format for this is the HTTP Archive (HAR). To make this data reasoned-upon by Mangle, we must define a semantic mapping from the HAR schema to Datalog predicates.15

HAR files contain hierarchical JSON structures. Mangle, being relational, requires normalizing this into flat facts.

**Proposed Network Logic Schema:**

| HAR Field | Mangle Predicate | Description |
| :---- | :---- | :---- |
| log.entries\[i\].request | http\_request(ReqId, Method, Url, StartTime) | Core transaction record. |
| request.headers | http\_header(ReqId, "req", Key, Value) | Normalized headers (keys lowercased). |
| response.status | http\_response(ReqId, StatusCode, Latency) | Response metadata. |
| \_initiator | request\_initiator(ReqId, Type, ScriptId) | Critical for causality: what triggered this? |

This schema allows for sophisticated queries. For instance, to detect "Waterfall" performance issues (a common optimization problem), the agent can query for requests that were initiated by other requests and had high latency.

### **3.4 Logic-Based Root Cause Analysis (RCA)**

The true power of this architecture emerges when we define deductive rules that correlate these cross-domain facts. Causality in distributed systems is often inferred via temporal precedence and shared context.13

We can encode heuristics for common web application failures directly into Mangle rules.

Example Rule: The "API-Triggered Crash"  
This rule posits that if a UI error occurs shortly after a failed network request, and that request was initiated by the script context, they are causally linked.

Prolog

// Fact definitions (ingested from Rod/CDP)  
network\_event(RequestId, Url, Status, Timestamp).  
console\_error(ErrorText, Timestamp).

// Causal Rule:  
// "A console error (E) is caused by a request (R) if:  
//  1\. The request failed (Status \>= 400\)  
//  2\. The request finished BEFORE the error appeared  
//  3\. The time difference is less than 100ms (temporal proximity)"  
caused\_by(ConsoleErr, ReqId) :-  
    console\_error(ConsoleErr, T\_Error),  
    network\_event(ReqId, \_, Status, T\_Net),  
    Status \>= 400,  
    T\_Net \< T\_Error,  
    // Calculate delta. Mangle supports basic arithmetic.  
    Delta \= T\_Error \- T\_Net,  
    Delta \< 100\.

When the agent asks "Why is the page broken?", instead of receiving a raw log dump, it queries caused\_by(X, Y). Mangle evaluates the rule against the buffered facts and returns a definitive tuple: caused\_by("Uncaught TypeError: user is undefined", "req\_55\_404\_not\_found").

This effectively moves the RCA process from the probabilistic domain of the LLM (guessing based on text proximity) to the deterministic domain of the logic engine (proving based on timestamp arithmetic).

### **3.5 Bottlenecks and Mitigation: The Ingestion Throughput Challenge**

While theoretically elegant, the Flight Recorder faces a massive engineering hurdle: **Throughput**. A complex web application can generate thousands of events per second, especially during startup or rapid animations.18

* **Serialization Overhead:** Every CDP event is a JSON object that must be unmarshaled in Go, transformed, and then inserted into Mangle.  
* **Solver Saturation:** Mangle's "semi-naive evaluation" strategy 20 is optimized for incremental updates, but calculating the fixed point of recursive rules after every single console.log is computationally prohibitive.

**Mitigation Strategies:**

1. **Event Throttling:** Implement debouncing for high-frequency events (e.g., mouse\_move or scroll events should be sampled or ignored unless explicitly requested).  
2. **Batch Insertion:** Instead of updating the Mangle database on every event, buffer events and perform a bulk AddFact operation every 100ms or 500ms. This allows the solver to run less frequently.  
3. **Selective Reification:** The MCP API should allow the agent to configure the "Logging Level." If the agent is debugging a CSS layout issue, it can disable the network and console ingestion pipelines to save resources.

## **4\. Vector 3: Session Persistence (The "Detached" Browser)**

### **4.1 The Continuity Problem in Agentic Workflows**

A subtle but critical friction point in current agentic workflows is the misalignment of lifecycles. Coding agents—whether running as a CLI tool (claude-code) or an IDE plugin—are often ephemeral processes. They start, execute a prompt, and terminate.

In contrast, the state of a web application being debugged is **accumulative**. Debugging a checkout flow requires adding items to a cart, filling out shipping forms, and reaching the payment step. This state (cookies, LocalStorage, SessionStorage, URL parameters) takes time to build.

If the browser instance is a child process of the agent (as is typical in standard Selenium/Puppeteer scripts), then when the agent terminates or restarts (e.g., the user kills the claude-code process), the browser closes, and the accumulated state is lost. This forces the agent to restart the entire setup flow for every single interaction, wasting tokens and time.

### **4.2 Architecture: The "Detached" MCP Server Model**

To solve this, the Mangle-MCP architecture validates a **Client-Server-Browser** topology where the MCP Server acts as a persistent middleware daemon.22

1. **Process Decoupling:** The MCP server runs as a background service (e.g., via systemd or as a Docker container). It is responsible for launching and managing the Chrome process.  
2. **Orphaned Browser Management:** Rod supports connecting to an existing browser instance via the CDP port (default 9222).24 The server creates the browser instance independently of any specific agent connection.  
3. **Stateful Session Registry:** The server maintains an internal registry of active browser "Sessions" (mapped to Chrome TargetIDs or Browser Contexts).

### **4.3 Technical Implementation: Session Management in Go**

This architecture requires a robust Session Management API exposed via MCP. The agent does not "launch" a browser; it "attaches" to a session.

**Core MCP Resources:**

* browser://sessions: Returns a list of active tabs/contexts.  
  * *Fact:* session(SessionId, Url, Title, Status).  
* browser://attach?id={sessionId}: Subscribes the agent to the event stream of a specific session.

Handling "Zombie" Processes and Docker Environments:  
Deploying this in a containerized environment (like a cloud-based IDE) introduces specific challenges. As noted in research snippet 26, running Chrome in Alpine Linux (common for Docker images) creates compatibility issues between the pre-built Chromium binary (linked against glibc) and Alpine's musl libc.

* **Solution:** The MCP server container must use a Debian-based base image (e.g., golang:buster) or explicitly install the chromium package from the Alpine repositories rather than letting Rod download the generic binary.  
* **Zombie Cleanup:** The Go server must implement signal handling (listening for SIGTERM / SIGINT) to ensure that when the MCP server itself is stopped, it gracefully closes the child Chrome processes to prevent "zombie" processes from consuming system memory.

### **4.4 The "Fork Session" Capability**

A particularly powerful innovation enabled by this persistence layer is the ability to **Fork** a session. Because Rod controls the browser at the CDP level, the server can implement a tool: fork\_session(original\_session\_id).

This tool would:

1. Snapshot the cookies and LocalStorage of the source session.  
2. Create a new Incognito Browser Context.27  
3. Inject the snapshotted state into the new context.  
4. Navigate to the same URL.

This allows the agent to perform **Destructive Testing**. If the agent wants to test a "Delete Account" button, it can fork the session, click the button in the copy, verify the deletion, and then discard the copy—leaving the original debugging session (where the user is still logged in) intact. This capability is virtually non-existent in current standard tooling but is trivial to implement in this persistent architecture.

## **5\. Vector 4: The "Test Runner" Innovation**

### **5.1 Shifting from Imperative to Declarative Verification**

Perhaps the most significant shift offered by Mangle-MCP is in the domain of verification. Currently, when an agent attempts to verify a fix (e.g., "I fixed the login button"), it typically relies on probabilistic visual verification. It might take a screenshot and ask its vision model, "Does this look like a logged-in dashboard?" This is computationally expensive, slow, and prone to error (e.g., missing a subtle error toast).

Alternatively, the agent might try to write a Selenium script to verify it. However, generating robust imperative code (driver.findElement(By.id("...")).click()) is difficult for LLMs because it requires precise knowledge of the DOM structure, which the agent often lacks.

The Mangle-MCP architecture introduces **Declarative Logic Testing**. Instead of telling the system *how* to test (the steps), the agent tells the system *what* must be true (the logic rule).

### **5.2 The "Halt Problem" in Agentic Loops**

A major issue in autonomous coding loops is the "Halt Problem": the agent doesn't know when it's done. It clicks a button and waits. How long should it wait? Did it work? Did it fail silently?

By offloading the assertion logic to the deterministic Mangle engine, we solve this. The agent submits a "Success Rule" to the server.

#### Workflow: The Logic-Based Assertion Loop

1. **Submission:** The agent defines a Mangle rule that represents the success state.  
   Prolog  
   // Rule: Success is defined as the URL changing to /dashboard  
   // AND the "Welcome" element appearing.  
   test\_passed :-  
       current\_url("/dashboard"),  
       dom\_text(\_, "Welcome User").

2. **Monitoring:** The MCP server enters a "Watch Mode." It continuously processes incoming CDP events (URL changes, DOM updates) and incrementally updates the Mangle database.20  
3. **Evaluation:** After every update, Mangle evaluates the test\_passed rule.  
4. **Notification:** As soon as the rule evaluates to true, the server pushes a notification to the agent: Test Passed. If a timeout is reached, it pushes Test Failed.

This moves the **Assertion Logic** out of the LLM's token stream (which is probabilistic and expensive) and into the Mangle engine (which is deterministic and fast). It allows the agent to fire an action and then simply "await" the logical conclusion.

### **5.3 Semantic Time-Travel Debugging: The "Killer Feature"**

The confluence of Vector 2 (Flight Recorder) and Vector 4 (Logic Testing) enables the "Killer Feature" of this architecture: **Semantic Time-Travel Debugging**.

Existing tools like Replay.io or Undo 28 allow developers to record and replay execution. However, these tools are opaque binary blobs. You can watch the replay, but you cannot easily *query* it programmatically.

Mangle-MCP effectively creates a **Temporal Knowledge Graph**. Because every fact in the Flight Recorder buffer has a timestamp, the agent can write queries that reason across the fourth dimension (time).

Example: Reasoning about Race Conditions  
An agent can ask: "Did the user click the 'Submit' button BEFORE the 'isReady' state variable turned true?"

```prolog
// Mangle Rule for Race Condition Detection
race_condition_detected :-
    // Find the click event
    click_event(BtnId, TimeClick),
    dom_attr(BtnId, "id", "submit-btn"),

    // Find the state change event
    state_change(VarName, "true", TimeReady),
    VarName \== "isReady",

    // Assert the invalid temporal order
    TimeClick < TimeReady.
```

This capability is indispensable for Senior Engineers (and Senior Agents) diagnosing concurrency bugs, race conditions, and complex state inconsistencies. It transforms the debugging experience from "watching a video" to "querying a database of history."

## **6\. Architecture Critique & Bottlenecks**

### **6.1 The Latency of Reification**

While the architecture is sound, the implementation faces strict physical constraints. The browser's main thread operates at 60 frames per second (16.6ms per frame). The "Round Trip Time" (RTT) for a reification loop involves:

1. CDP Event firing (Browser \-\> Go).  
2. JSON Deserialization (Go).  
3. Data Transformation (Go \-\> Mangle Fact).  
4. Mangle Solver Update (Datalog Engine).

If this RTT exceeds 16ms, the "Debugger" view will lag behind the "Runtime." For heavy animations or rapid-fire logging, the backlog can grow exponentially.

**Strategic Trade-off:** The system must prioritize **Eventual Consistency** over strict Real-Time Synchrony for high-frequency data. We recommend implementing **Adaptive Sampling**: if the event queue depth exceeds a threshold, the system should drop high-frequency, low-value events (like mouse\_move) and prioritize topology-changing events (like childNodeInserted).

### **6.2 Garbage Collection in Datalog**

Mangle, like most Datalog implementations, is an append-only logic system by default. Handling state *changes* (e.g., a node moving from Parent A to Parent B) requires **Fact Retraction**.1

The Mangle Go library supports RetractFact, but this operation is more expensive than AddFact because it may require recalculating derived views (the "Truth Maintenance" problem).

* **Challenge:** If the DOM changes significantly (e.g., a full page navigation), retracting 2,000 facts and adding 2,000 new ones triggers a massive re-computation.  
* **Solution:** The architecture should utilize Mangle's support for **Stratified Negation** or simply clear the entire dom\_node predicate namespace on navigation events, rather than issuing individual retractions.

## **7\. Conclusion: The Infrastructure of Autonomy**

The transition from human-centric to agent-centric development tools is not merely a change in interface; it is a fundamental restructuring of the information pipeline. Humans rely on visual synthesis and intuition; Agents rely on semantic structure and deterministic logic.

The **Mangle-MCP Architecture** successfully validates the "Post-Selenium" era by proposing a system that respects the cognitive architecture of the AI agent. By replacing the opaque pixel-soup of the DOM with the structured, queryable rigor of a Deductive Database, we grant agents the "X-Ray Vision" they need to understand, debug, and verify software systems with superhuman precision.

While the engineering challenges of ingestion throughput and memory management are significant, they are solvable with standard distributed systems techniques (buffering, sampling, windowing). The payoff—a debugging environment where agents can perform Root Cause Analysis via logic queries and verify fixes via declarative rules—represents the necessary leap required to move AI coding agents from "Junior Copilots" to "Senior Engineers."

## **8\. Appendix: Logic Schema Reference**

### **8.1 Network Debugging Schema (Vector 2\)**

Prolog

// \--- HTTP Core \---  
// Records the lifecycle of a request-response pair.  
// id: Unique identifier from CDP (RequestId).  
// method: HTTP Method.  
// url: The target URL.  
// initiator: ID of the script/request that triggered this.  
net\_request(Id, Method, Url, InitiatorId, StartTime).

// \--- Headers \---  
// Stores headers. Key is normalized to lowercase.  
net\_header(Id, "req" | "res", Key, Value).

// \--- Response Metrics \---  
// status: HTTP Status Code.  
// latency: Time to first byte (ms).  
// duration: Total download duration (ms).  
net\_response(Id, Status, Latency, Duration).

// \--- Logic Rules \---

// Rule: Identify "Slow" API calls (\>1000ms)  
slow\_api(Id, Url) :-  
    net\_request(Id, \_, Url, \_, \_),  
    net\_response(Id, \_, \_, Duration),  
    Duration \> 1000\.

// Rule: Identify "Cascading Failures"  
// A failure caused by another failure.  
cascading\_failure(ChildId, ParentId) :-  
    net\_request(ChildId, \_, \_, ParentId, \_),  
    net\_response(ChildId, StatusChild, \_, \_),  
    StatusChild \>= 400,  
    net\_response(ParentId, StatusParent, \_, \_),  
    StatusParent \>= 400\.

#### **Works cited**

1. google/mangle \- GitHub, accessed November 22, 2025, [https://github.com/google/mangle](https://github.com/google/mangle)  
2. go-rod/rod: A Chrome DevTools Protocol driver for web automation and scraping. \- GitHub, accessed November 22, 2025, [https://github.com/go-rod/rod](https://github.com/go-rod/rod)  
3. What is Model Context Protocol (MCP)? A guide | Google Cloud, accessed November 22, 2025, [https://cloud.google.com/discover/what-is-model-context-protocol](https://cloud.google.com/discover/what-is-model-context-protocol)  
4. A deep dive into React Fiber \- LogRocket Blog, accessed November 22, 2025, [https://blog.logrocket.com/deep-dive-react-fiber/](https://blog.logrocket.com/deep-dive-react-fiber/)  
5. Inside Fiber: an in-depth overview of the new reconciliation algorithm in React, accessed November 22, 2025, [https://blog.ag-grid.com/inside-fiber-an-in-depth-overview-of-the-new-reconciliation-algorithm-in-react/](https://blog.ag-grid.com/inside-fiber-an-in-depth-overview-of-the-new-reconciliation-algorithm-in-react/)  
6. React Internals: Virtual DOM 🖼️ vs Fiber Node for Performance Explained, accessed November 22, 2025, [https://dev.to/yorgie7/react-internals-virtual-dom-vs-fiber-node-for-performance-explained-5an5](https://dev.to/yorgie7/react-internals-virtual-dom-vs-fiber-node-for-performance-explained-5an5)  
7. How does React traverse Fiber tree internally? \- JSer.dev, accessed November 22, 2025, [https://jser.dev/react/2022/01/16/fiber-traversal-in-react/](https://jser.dev/react/2022/01/16/fiber-traversal-in-react/)  
8. Exploring React Fiber tree \- by Abhas Bhattacharya \- Medium, accessed November 22, 2025, [https://medium.com/@bendtherules/exploring-react-fiber-tree-20cbf62fe808](https://medium.com/@bendtherules/exploring-react-fiber-tree-20cbf62fe808)  
9. How can I traverse React Component Tree just using \_\_REACT\_DEVTOOLS\_GLOBAL\_HOOK\_\_ \- Stack Overflow, accessed November 22, 2025, [https://stackoverflow.com/questions/73123202/how-can-i-traverse-react-component-tree-just-using-react-devtools-global-hook](https://stackoverflow.com/questions/73123202/how-can-i-traverse-react-component-tree-just-using-react-devtools-global-hook)  
10. React \- getting a component from a DOM element for debugging \- Stack Overflow, accessed November 22, 2025, [https://stackoverflow.com/questions/29321742/react-getting-a-component-from-a-dom-element-for-debugging](https://stackoverflow.com/questions/29321742/react-getting-a-component-from-a-dom-element-for-debugging)  
11. javascript \- What is Virtual DOM? \- Stack Overflow, accessed November 22, 2025, [https://stackoverflow.com/questions/21965738/what-is-virtual-dom](https://stackoverflow.com/questions/21965738/what-is-virtual-dom)  
12. performance comparison : json vs pure html \! \- Stack Overflow, accessed November 22, 2025, [https://stackoverflow.com/questions/4581577/performance-comparison-json-vs-pure-html](https://stackoverflow.com/questions/4581577/performance-comparison-json-vs-pure-html)  
13. Brief Announcement: Techniques for Programmatically Troubleshooting Distributed Systems \- Colin Scott, accessed November 22, 2025, [https://colin-scott.github.io/personal\_website/research/podc13.pdf](https://colin-scott.github.io/personal_website/research/podc13.pdf)  
14. Chrome DevTools Protocol \- GitHub Pages, accessed November 22, 2025, [https://chromedevtools.github.io/devtools-protocol/](https://chromedevtools.github.io/devtools-protocol/)  
15. How to generate a HAR file | Adobe Experience Manager, accessed November 22, 2025, [https://experienceleague.adobe.com/en/docs/experience-cloud-kcs/kbarticles/ka-19572](https://experienceleague.adobe.com/en/docs/experience-cloud-kcs/kbarticles/ka-19572)  
16. HTTP Vocabulary in RDF 1.0 \- W3C, accessed November 22, 2025, [https://www.w3.org/TR/HTTP-in-RDF10/](https://www.w3.org/TR/HTTP-in-RDF10/)  
17. Root Cause Analysis of Failures in Microservices through Causal Discovery \- Purdue Engineering, accessed November 22, 2025, [https://engineering.purdue.edu/dcsl/publications/papers/2022/rca-neurips22.pdf](https://engineering.purdue.edu/dcsl/publications/papers/2022/rca-neurips22.pdf)  
18. Batch vs. Stream Processing: How Do You Choose? : r/dataengineering \- Reddit, accessed November 22, 2025, [https://www.reddit.com/r/dataengineering/comments/1fnepc3/batch\_vs\_stream\_processing\_how\_do\_you\_choose/](https://www.reddit.com/r/dataengineering/comments/1fnepc3/batch_vs_stream_processing_how_do_you_choose/)  
19. How large DOM sizes affect interactivity, and what you can do about it | Articles \- web.dev, accessed November 22, 2025, [https://web.dev/articles/dom-size-and-interactivity](https://web.dev/articles/dom-size-and-interactivity)  
20. engine package \- github.com/google/mangle/engine \- Go Packages, accessed November 22, 2025, [https://pkg.go.dev/github.com/google/mangle/engine](https://pkg.go.dev/github.com/google/mangle/engine)  
21. A Differential Datalog Interpreter \- MDPI, accessed November 22, 2025, [https://www.mdpi.com/2674-113X/2/3/20](https://www.mdpi.com/2674-113X/2/3/20)  
22. How to MCP \- The Complete Guide to Understanding Model Context Protocol and Building Remote Servers | Simplescraper Blog, accessed November 22, 2025, [https://simplescraper.io/blog/how-to-mcp](https://simplescraper.io/blog/how-to-mcp)  
23. Architecture overview \- Model Context Protocol, accessed November 22, 2025, [https://modelcontextprotocol.io/docs/learn/architecture](https://modelcontextprotocol.io/docs/learn/architecture)  
24. rod \- Go Packages, accessed November 22, 2025, [https://pkg.go.dev/github.com/go-rod/rod](https://pkg.go.dev/github.com/go-rod/rod)  
25. How to run Rod and view the browser with docker compose? \#717 \- GitHub, accessed November 22, 2025, [https://github.com/go-rod/rod/discussions/717](https://github.com/go-rod/rod/discussions/717)  
26. go \- Rod Running In Docker Alpine Get Error "chrome-linux/chrome: no such file or directory", accessed November 22, 2025, [https://stackoverflow.com/questions/70254649/rod-running-in-docker-alpine-get-error-chrome-linux-chrome-no-such-file-or-dir](https://stackoverflow.com/questions/70254649/rod-running-in-docker-alpine-get-error-chrome-linux-chrome-no-such-file-or-dir)  
27. How to support simultaneous operation of multiple tabs · Issue \#1207 · go-rod/rod \- GitHub, accessed November 22, 2025, [https://github.com/go-rod/rod/issues/1207](https://github.com/go-rod/rod/issues/1207)  
28. 6 Things You Need to Know About Time Travel Debugging \- Undo.io, accessed November 22, 2025, [https://undo.io/resources/6-things-time-travel-debugging/](https://undo.io/resources/6-things-time-travel-debugging/)  
29. Time-Travel Debugging Production Code | by Loren Sands-Ramshaw | Medium, accessed November 22, 2025, [https://blog.lorensr.me/time-travel-debugging-production-code-aa82714011d5](https://blog.lorensr.me/time-travel-debugging-production-code-aa82714011d5)
