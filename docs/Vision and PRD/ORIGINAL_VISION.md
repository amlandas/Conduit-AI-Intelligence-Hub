# Original Vision Document

> **Note**: This document captures the original product vision from December 2025. The actual V1 implementation pivoted to focus on **Private Knowledge Base** functionality, deferring the connector marketplace features to future versions. See [PRD.md](PRD.md) for the current product requirements.

---

**Original Document Below**

---

## Executive takeaway

Here’s PRD v1 for Simpleflo Conduit — AI Intelligence Hub: a security-first, user-owned hub that (1) discovers/installs/updates MCP servers across AI clients, and (2) turns a user’s documents into a private local knowledge base exposed through a first-party KB MCP server—with container isolation so “malicious connector” risk is bounded. It preserves the strongest spine of your old PRD—Intent → Recommend → Audit → Install → Inject → Lifecycle  and extends it to knowledge onboarding + client-specific connectivity (ChatGPT requiring a public HTTPS /mcp endpoint, typically via tunnel, is explicitly handled)  .

---

# Product Requirements Document (PRD) — Simpleflo Conduit

Product: Simpleflo Conduit
Expanded title: AI Intelligence Hub
Version: v1.0 (First full PRD draft)
Status: Draft (for iteration)
Last updated: 2025-12-27
Owner: AD
Platforms: macOS, Windows, Ubuntu
Primary users: Power users / researchers; secondary AI PM / Technical PM
Design intent: “Command Center” feel (lightweight, OS-native utility) 

---

## 1. Background and Context

### 1.1 What Conduit is

Conduit is a native application that helps users safely connect AI tools to:
- External tools (via MCP servers)
- Private knowledge (user documents transformed into a local knowledge base)
- Cloud sources (connect where the data already lives; do not “force local”)
Conduit removes undifferentiated setup work: discovery, configuration, dependency management, security review, client injection, updates, and vulnerability monitoring.

### 1.2 Why now

MCP is emerging as a standard interface for connecting AI assistants to tools and data. Many clients support local MCP servers (Claude Desktop, etc.)  , while ChatGPT’s connector flow requires a public HTTPS /mcp endpoint, commonly achieved via a tunnel during development.

But, the AI tools lack the personal context with which humans operate. They need to repeatedly upload documents, manually connect data sources, or tools to be really productive with their AI tools in the environment in which they operate. Bringing the power of the personal context in which people work to their AI tools through MCP connectivity makes their AI tools more personalized to their operating environment.

---

## 2. Vision

### 2.1 Vision statement

Become the user-owned “control plane” for AI tools and personal knowledge: secure connectivity, reliable configuration, and easy lifecycle management—without wiring.

### 2.2 Core product thesis

“App Store + Firewall + Private Knowledge Bridge.”

---

## 3. Goals, Non-Goals, and Principles

### 3.1 Goals (v1)

1. Fast time-to-value: A user goes from a plain-English intent to a working integration in minutes (not hours).
2. Private knowledge in AI tools: Users can bring their own documents and have them usable via a first-party local KB MCP server.
3. Security and privacy by default: Third-party MCP servers are treated as untrusted; Conduit isolates execution and protects secrets.
4. Multi-client injection: Configure once, propagate to multiple AI clients (desktop/CLI/IDE).
5. Lifecycle management: Updates, pinning, vulnerability watch, and replacement guidance.

### 3.2 Non-goals (v1)

- Enterprise multi-user RBAC / org policy engines / SOC2-grade admin consoles (explicitly later)
- Hosting or syncing user-imported local documents to Simpleflo cloud by default (local stays local)
- Building a general “agent runtime” (Conduit is a hub + connector manager + KB bridge)

### 3.3 Principles

- Secure by default: isolate, least privilege, visible permissions, safe failure modes
- Zero-touch setup where possible: install/manage runtimes and containers with minimal user intervention (one-time approvals are acceptable)
- Client-specific pragmatism: different AI tools require different connection methods (local config vs remote HTTPS endpoints)   
- Clarity beats cleverness: defaults should work; advanced controls can come later

---

## 4. Target Users and Personas

### Persona A: Power user / researcher (Primary)
A technically comfortable user who uses multiple AI tools and wants reliable, safe integrations without spending time chasing config formats, dependency issues, and security risks.

Top needs
- One-click installs and safe defaults
- Ability to bring private documents into AI workflows
- Trust signals and safety checks before installing third-party MCP servers 
- Cross-tool consistency (“configure once”)  

### Persona B: AI PM / Technical PM (Secondary)

A user who needs quick enablement, repeatability, and confidence in privacy/security without becoming a full-time integrator.

Top needs
- Works reliably across multiple tools
- Clear security posture and transparency
- Minimal maintenance burden

---

## 5. User-Facing Outcomes and Key Use Cases

### 5.1 Use cases

1. Connect tool → AI client  
    “Connect Claude Desktop to Notion so I can read/write pages from chat.”     
2. Connect private docs → AI client  
    “Use my local project docs as a private knowledge base from Claude Code / Cursor.”
3. Cross-client parity  
    “If it works in Claude Code, I want it to also work in Cursor and VS Code without redoing setup.” 
4. Safety and hygiene  
    “Warn me if a connector looks dangerous or becomes vulnerable later.” 

---

## 6. End-to-End UX Flows (v1)

### UX Loop A — Intent → Recommendation → Safe Install → Works in client(s)

1. User types an intent: “Connect to Jira; I need to read issues and create comments.”
2. Conduit asks clarifying questions when needed.
3. Conduit recommends the best MCP server by default and shows alternatives on request.
4. Conduit shows trust signals: community metrics + audit status  (expanded in Section 9).
5. Conduit installs runtime/dependencies automatically (Node/Python/Go, etc.).
6. Conduit configures the integration across selected clients (“inject once, sync many”).
7. User validates: tool call succeeds inside the AI client.  

### UX Loop B — Bring docs → Private KB → Connect KB to AI tools (updated for ChatGPT reality)

1. User selects sources (local folders/files; optional cloud sources remain cloud-hosted).
2. Conduit ingests documents and builds a Private KB with a profile-based default (“Notes”, “Technical Docs”, “Mixed Workspace”).
3. Conduit provisions the first-party Private KB MCP server, running in a sandboxed container.
4. Conduit connects the KB MCP server to AI clients using client-specific methods:
	- Local MCP clients: configure local servers via client config (Claude Desktop supports local MCP server connections) 
	- Perplexity Mac (local): requires installing a helper app to securely connect to local MCP servers 
	- ChatGPT (remote HTTPS): requires a public HTTPS /mcp endpoint (often via ngrok/Cloudflare Tunnel in dev)  . Conduit provides a “Secure Link” flow to create/manage this safely.
5. User validates with a guided “Ask your KB” test prompt and sees a simple trace (what connector was used, what tool ran).

### UX Loop C — Stay safe over time (maintenance)
- Conduit watches for connector vulnerabilities and prompts: Update/Patch/Replace 
- Conduit provides one dashboard to update/pin versions across all installed servers.  

---

## 7. Product Scope: What’s in v1

### 7.1 Supported client scope (v1 targets)

Desktop apps: ChatGPT, Claude Desktop, Perplexity
CLI apps: Codex, Gemini CLI, Claude Code, Cline, Kiro-CLI, Aider, Cursor CLI
IDEs: VS Code (+ Copilot/Cline), Cursor, Kiro, Antigravity
(Implementation will be staged; see Release Plan)

---

## 8. User Stories (written in full English)

### Epic 1: Discover and choose the right connector

1. Natural Language Intent  
	* As a power user, I want to describe what I’m trying to do in plain English, so that I don’t have to search GitHub and compare multiple MCP servers myself.  
    
	* Acceptance criteria    
		* Conduit accepts a free-form intent and returns a recommended MCP server within 3 seconds for cached index hits (see NFRs).
		* If the intent is ambiguous, Conduit asks a clarifying question (e.g., read vs write permissions).
		* User can request alternatives and compare at least 3 options.
    
2. Bring your own registry  
	* As a power user, I want to connect my own registry (private GitHub/org source), so that I can use vetted internal connectors instead of only public ones.  
	* Acceptance criteria
		- User can add a registry source and toggle it on/off.
		- Registry items are searchable and clearly labeled by origin.
    
### Epic 2: Install safely and run in isolation

3. Audit before install  
	* As a security-conscious user, I want Conduit to scan a connector before installation, so that I don’t accidentally run dangerous code on my machine.  
	* Acceptance criteria
		- Conduit performs static checks for suspicious domains, dangerous shell commands, and obfuscation.
		- Conduit presents a clear “Audit Result” badge (Pass / Warn / Block) separate from community trust (see Epic 6).
		- If blocked, Conduit explains why and offers safe alternatives.
    
4. Containerized execution by default  
	* As a privacy-focused user, I want third-party MCP servers to run in isolated containers by default, so that a malicious server cannot access my broader system.  
	* Acceptance criteria
		- Newly installed third-party servers run in a sandboxed container by default.
		- The container has no access to host filesystem unless explicitly granted.
		- Network access is scoped based on declared connector needs (v1 may start with simple allow/deny + documented default policy).
    
5. Zero-touch runtime setup  
	* As a user, I want Conduit to install and configure required runtimes and container tooling for me, so that I don’t need to troubleshoot dependencies.  
	* Acceptance criteria
		- Conduit automatically installs required language runtimes/deps for selected MCP servers where feasible.
		- If a one-time system approval is required, Conduit explains it in plain English and proceeds after approval.
    

### Epic 3: Manage credentials safely

6. Guided credential setup  
	* As a user, I want Conduit to guide me to obtain required API keys, so that I can finish setup without guessing where credentials live.  
	* Acceptance criteria
		- Conduit links directly to the relevant provider’s API key page and shows step-by-step instructions.
		- Conduit provides a secure input field that writes the key into the right config without the user editing hidden JSON files.  
      
7. Local secret storage  
	* As a user, I want my secrets stored locally in my OS keychain, so that Conduit never needs to store credentials in the cloud.  
	* Acceptance criteria
		- Keys are stored in OS keychain (macOS Keychain, Windows Credential Manager, Linux secret store equivalent).
		- Conduit never transmits secrets to Simpleflo servers.  
      
### Epic 4: Connect once, use everywhere

8. Multi-client injection  
	* As a user who uses multiple AI tools, I want Conduit to configure connectors across those tools automatically, so that I don’t repeat setup per client.  
	* Acceptance criteria
		- User can select multiple clients, and Conduit writes required configuration for each supported client reliably.
		- Conduit validates configuration and provides a “Test connection” workflow.  
      
### Epic 5: Private knowledge base (first-party)

9. Add documents and build a Private KB  
	* As a user, I want to add local files and folders to Conduit, so that my AI tools can answer questions using my private documents.  
	* Acceptance criteria
		- User can add/remove sources and trigger re-index.
		- Conduit provides profile presets (Notes / Technical Docs / Mixed) and a recommended default.  
      
10. First-party Private KB MCP server  
	* As a user, I want Conduit to automatically create and maintain a local KB MCP server, so that I do not have to run or code my own server.  
	* Acceptance criteria
		- Conduit can start/stop the KB MCP server and show its health.
		- KB server runs in an isolated container by default.
		- AI tools can query and receive structured answers with citations (response format to be finalized in design/LLD).
    
11. ChatGPT Secure Link for KB access  
	* As a ChatGPT user, I want a safe way to connect ChatGPT to my local KB, so that I can use my private knowledge in ChatGPT without manually configuring tunnels.  
	* Acceptance criteria
		- Conduit can create a “Secure Link” that exposes a public HTTPS /mcp endpoint suitable for ChatGPT connector configuration  .
		- Conduit provides clear warnings about what becomes reachable and provides a one-click revoke/disable.
		- Conduit offers authentication options (v1 can start with a simple token header + rotation).  
      
### Epic 6: Trust and lifecycle management

12. Trust score with separate audit status  
	* As a user, I want to see both community trust signals and Conduit’s audit result separately, so that I can make an informed decision.  
	* Acceptance criteria
		- Community Trust Score uses signals like stars/downloads/recency  .
		- Audit status is clearly distinct and explains findings.  

13. Vulnerability watch and updates  
	* As a user, I want Conduit to notify me when a connector becomes vulnerable or outdated, so that I stay safe over time.  
	* Acceptance criteria
		- Conduit can alert on new vulnerabilities and provide Update/Patch/Replace suggestions  .
		- User can pin versions and roll back if an update breaks.      

---

## 9. Functional Requirements (system view)

### 9.1 Conduit Global Indexer (CGI) — “Brain”

- Hybrid search: daily cached index for sub-second search + real-time deep search fallback when needed.    
- Connector metadata: description, capabilities, required permissions, dependencies
- Registry toggles (public/community/private) 
    
### 9.2 Conduit Auditor — “Firewall”

- Static code analysis checks for suspicious domains/commands/obfuscation 
- Produces an Audit Signal (Pass/Warn/Block) separate from Community Trust  
### 9.3 Conduit Installer + Runtime Manager

- Dependency automation (Node/Python/Go; server deps) 
- Container runtime setup with minimal user friction
- Default isolation policy for third-party servers  

### 9.4 Conduit Injector — “Configure once”

- Config-writing logic for priority clients (staged rollout) 
- Validation + test flow per client  

### 9.5 Private KB Builder + First-party KB MCP Server

- Document ingestion with KB profiles (sane defaults)
- Local model can assist structuring/normalization (implementation detail; not user-facing)
- KB MCP server runs locally (containerized) and is maintained automatically 

### 9.6 Secure Link (for ChatGPT and other remote-URL clients)

- Create and manage a public HTTPS /mcp endpoint for ChatGPT connectors 
- Support “revoke/disable instantly”
- Authentication support (token header v1; stronger auth later)  

---

## 10. Non-Functional Requirements (v1)

- Performance: Cached connector search returns results in < 3s for common intents     
- Reliability: Background agent survives reboots to maintain auto-update and vulnerability watch 
- Compatibility targets (from old PRD, adapted): Support modern model ecosystems; local models supported via standard MCP JSON 
- Security baseline: local secrets only; sandbox execution; least privilege by default  

---

## 11. Metrics and Success Criteria

### North Star Metric

Time-to-Useful-Connection (TTUC): time from “intent entered” to “first successful tool invocation” inside a chosen AI client.
### Supporting metrics
- Install success rate by OS + client
- % setups completed without manual file edits
- Audit outcomes: pass/warn/block distribution; false positive/negative review rate
- KB metrics: ingestion success rate; time to first query; citation presence rate
- Retention proxy: weekly active connectors per user; number of connected clients per user  

---

## 12. Release Plan (incremental delivery that is not gimmicky)

### V0 — CLI-first “Conduit Engine”

Why: validates the engine (auditing, sandboxing, KB server, injection) before GUI polish.
V0 must feel complete
- CLI can: discover/install/audit/run connectors; manage secrets; inject into 2–3 priority clients deeply; run KB MCP server and connect it locally.
- Includes status/logs and “test connection” flows.  

### V0.5 — Trust + Secure Link

- Add Secure Link flow for ChatGPT connector requirements (public HTTPS /mcp) 
- Expand client coverage (more CLI tools and at least one IDE flow)  

### V1 — Desktop apps (macOS/Windows/Ubuntu)

- GUI “Command Center”     
- Connector directory, permissions UI, lifecycle dashboard, KB onboarding wizard
- Desktop-specific flows (Claude Desktop extensions directory inspiration; Perplexity helper pattern)  

---

## 13. Risks and Mitigations

1. Security risk: malicious connector escapes sandbox
	- Mitigate: strict container isolation defaults, minimal host mounts, transparent permissions UI, staged rollout
2. User friction: container runtime setup
	- Mitigate: guided one-time approval, automated setup, clear recovery steps
3. Client variability: each AI tool differs
	- Mitigate: “client adapters” architecture; tiered support (best-effort vs first-class)
4. ChatGPT remote endpoint requirement adds complexity
	- Mitigate: Secure Link feature; clear warnings + revoke; optional hosted endpoint later  

---

## 14. Open Questions (for v1.1 iteration)

- Minimum viable response schema from KB MCP server (citations, structured output format)
- Default KB profile definitions (what profiles, what heuristics)
- Container runtime implementation choice per OS (how “zero-touch” can truly be)
- How much automated key acquisition is safe/appropriate  

---

# Appendix A — Relevant foundations

- Enhanced user stories for discovery/intent, clarification, alternatives 
- Universal sync / injection across multiple clients 
- Dependency automation 
- BYO registry 
- Vulnerability monitoring + lifecycle dashboard 
- Hybrid cached index + deep search fallback 
- Static code analysis checks 
- Local vault secrets in OS keychain 
- Roadmap “Brain / Auditor / Injector / Dashboard” pattern  

---

## Next step (to iterate efficiently)

The highest-leverage next iteration is to make it shippable-engineering precise by adding:
1. a capability matrix (client × transport × injection method)
2. a permissions model (what a connector can access and how it’s granted)
3. v0/v0.5/v1 acceptance criteria tables (so scope stays lean)  

  
**