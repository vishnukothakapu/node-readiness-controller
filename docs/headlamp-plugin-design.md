# UI Design Document: Node Readiness Controller

### LFX Project: Headlamp Plugin for Node Readiness Controller (Term 3)

This project brings native UI visibility to the Node Readiness Controller (NRC) via a [Headlamp](https://headlamp.dev/) plugin. This is a continuation of the Term 2 observability project, which delivered the metrics foundation, scrape-time collectors, and Grafana dashboard.

The project answers two main questions:

1. **Per-object readiness state:** Why is this specific node not accepting workloads? Which rule, condition, or taint is holding it back? Today this requires `kubectl describe node`, controller logs, or raw YAML parsing.
2. **Fleet-wide rule enforcement:** Across all rules, how many nodes are matched, held, or released? Which rules are failing and where? Today this is answered only through Prometheus queries and the Grafana dashboard built in Term 2.

## Goals

* Build a Headlamp plugin to visualize cluster-wide rule enforcement and per-node readiness state.
* Provide cluster-level views: all `NodeReadinessRule` objects with targeted, satisfied, unsatisfied, and failed node counts per rule.
* Provide node-level detail views: which rules apply to a node, per-condition status, evaluation reasons, and NRC-managed taints.
* Build a unified lifecycle-conditions panel showing Node conditions referenced by NRC rules alongside standard kubelet conditions.
* Serve as an early consumer of the upcoming [`NodeReadinessEvaluation`](https://github.com/kubernetes-sigs/node-readiness-controller/pull/345) CRD, reporting bugs and design gaps back to the NRC controller team.
* Minimize Kubernetes API overhead. Watch-based, API-state only.
* Validate and document UX and scale tests / limits for the plugin.
* Publish to Headlamp Artifact Hub with documentation.

## Non-Goals

* Build a standalone backend API or web server. The plugin runs entirely inside Headlamp.
* Replace the Prometheus metrics and Grafana dashboard from Term 2. Headlamp gives visibility into per-object questions; Grafana answers fleet-wide SLO questions over time.
* Implement custom WebSocket or polling mechanisms. We rely on Headlamp's built-in watch hooks.

## Relationship to Term 2 Observability

The Term 2 observability project delivered Prometheus metrics (`node_readiness_rule_nodes{state}`, `node_readiness_rule_matched_nodes`, etc.) via scrape-time collectors and a Grafana dashboard for fleet-wide SLO monitoring. This plugin complements that work by answering **per-object** questions that metrics cannot: _which_ node is failing _which_ rule's _which_ condition, and what the failure reason is. The two systems serve different personas, Grafana for SREs monitoring trends over time, Headlamp for operators debugging a specific node right now.

---

## Design

### 1. Personas

We design the UI around the same distinct personas used in the controller observability design.

1. **Infrastructure Owners (Cluster Operators):** They run the cluster and manage the node lifecycle. They need a Landing Page to confirm the controller's resources are available and get high-level rule visibility before drilling into individual rules.
2. **Component / Rule Owners:** They own the infrastructure components that gate node readiness (CNI, GPU drivers, CSI). They need the Rule Details View to see how their rule propagates. *(Note: For per-node evaluation details, the plugin will use NRE as the authoritative source once the NRE API is available.)*
3. **Workload Owners (Application Developers):** They run pods. They need the Node Detail Extension and the `NodeReadinessEvaluation` (NRE) Details View to understand exactly which conditions or rules are preventing their pods from scheduling on a specific node.

---

### 2. Code Organization

The project spans two repositories. All plugin UI code (React components, styles, tests) lives in the [Headlamp plugins repository](https://github.com/headlamp-k8s/plugins/tree/main/node-readiness-controller). Design documentation and any backend API changes live in the [NRC repository](https://github.com/kubernetes-sigs/node-readiness-controller).

| Repository | Contents |
|---|---|
| `headlamp-k8s/plugins/node-readiness-controller` | Plugin source (`src/`), `package.json`, unit tests, `CODEOWNERS`, Artifact Hub metadata. Reviewed by Headlamp and NRC maintainers. |
| `kubernetes-sigs/node-readiness-controller` | This design document (`docs/`), backend changes (e.g., `status.nodeEvaluations` on `NodeReadinessRule`). Reviewed by NRC maintainers. |

Plugin development work is tracked against [Issue #327](https://github.com/kubernetes-sigs/node-readiness-controller/issues/327) for traceability.

---

### 3. CRD Data Surface

The plugin reads from the `readiness.node.x-k8s.io/v1alpha1` API group. Both custom resources are cluster-scoped.

#### 3.1 NodeReadinessRule Fields Consumed

| JSON Path | Type | Used In |
|---|---|---|
| `metadata.name` | `string` | Rule List, Rule Details |
| `metadata.creationTimestamp` | `Time` | Rule List (Age column), Rule Details |
| `metadata.deletionTimestamp` | `Time` | Rule List (Terminating status indicator) |
| `spec.enforcementMode` | `"bootstrap-only"` or `"continuous"` | Rule List (chip), Rule Details |
| `spec.dryRun` | `bool` | Rule List (warning chip), Rule Details |
| `spec.nodeSelector.matchLabels` | `map[string]string` | Rule List, Rule Details |
| `spec.nodeSelector.matchExpressions` | `[]LabelSelectorRequirement` | Rule List, Rule Details |
| `spec.conditionPolicy` | `"allOf"` or `"anyOf"` | Rule List (chip), Rule Details |
| `spec.taint.key` | `string` | Rule List, Rule Details |
| `spec.taint.value` | `string` (optional) | Rule List, Rule Details |
| `spec.taint.effect` | `"NoSchedule"` / `"PreferNoSchedule"` / `"NoExecute"` | Rule List (chip), Rule Details |
| `spec.conditions[].type` | `string` | Rule Details (Conditions Table) |
| `spec.conditions[].requiredStatus` | `"True"` / `"False"` / `"Unknown"` | Rule Details (Conditions Table) |
| `spec.conditions[].defaultStatus` | `"True"` / `"False"` / `"Unknown"` | Rule Details (Conditions Table) |
| `status.dryRunResults.affectedNodes` | `*int32` | Rule Details (Dry-Run section) |
| `status.dryRunResults.taintsToAdd` | `*int32` | Rule Details (Dry-Run section) |
| `status.dryRunResults.taintsToRemove` | `*int32` | Rule Details (Dry-Run section) |
| `status.dryRunResults.riskyOperations` | `*int32` | Rule Details (Dry-Run section) |
| `status.dryRunResults.summary` | `string` | Rule Details (Dry-Run section) |
| `status.nodeEvaluations` | `[]NodeEvaluation` | Rule List, Rule Details, Landing Page |
| `status.failedNodes` | `[]NodeFailure` | Rule List, Rule Details, Landing Page |
| `status.appliedNodes` | `[]string` | Rule Details |

> **Dependency Note:** The initial plugin implementation may compute lightweight aggregated statistics from the existing `status.nodeEvaluations` data. If the upstream `status.evaluationSummary` API becomes available, the plugin can consume those server-provided aggregates instead. Per-node evaluation details will transition to the `NodeReadinessEvaluation` CRD once that API is available.

#### 3.2 NodeReadinessEvaluation Fields Consumed

The `NodeReadinessEvaluation` (NRE) CRD is a per-node evaluation object currently under development ([PR #345](https://github.com/kubernetes-sigs/node-readiness-controller/pull/345)). The current project plan is targeted for an upcoming NRC release containing the NRE API. NRE-dependent features will be enabled once the CRD is merged and available in the target release.

The Headlamp plugin is intended to serve as an early consumer of the NRE API, providing an opportunity to validate the API through real UI usage while the NRE API remains experimental. The NRE views (Sections 7.4 and 7.5) will be implemented once NRE merges.

NRE availability also depends on the NRC NRE feature being enabled. The current PR #345 implementation is opt-in via the `--enable-node-readiness-evaluation` controller flag. Therefore, the plugin must treat "NRE CRD installed" and "NRE evaluation data available" as separate states.

The NRE API is under active development in [PR #345](https://github.com/kubernetes-sigs/node-readiness-controller/pull/345). The following UI contract is based on the current NRE proposal in PR #345 and remains subject to upstream API changes.

The plugin requires the NRE API to expose, at minimum:

- target node identity
- overall evaluation state
- per-rule evaluation status
- relevant reason/message information
- evaluation timestamps

Exact field names and additional metadata remain subject to the final NRE API.

**Future/proposed NRE integration:** Support explicit `DryRun` evaluation metadata and `ObservedRuleGeneration` if upstream adds them to `RuleEvaluation`. Until then, Dry-Run remains represented by the NRR's `spec.dryRun` and existing `status.dryRunResults`.

#### 3.3 Kubernetes Node Object Fields Consumed

| JSON Path | Used For |
|---|---|
| `spec.taints[]` | Filtering taints with `readiness.k8s.io/` prefix |
| `status.conditions[]` | Grouping Node conditions referenced by NRC rules vs standard kubelet conditions |

---

### 4. Integration Architecture

The plugin is a standard Headlamp extension built with React and TypeScript (`@kinvolk/headlamp-plugin`). 

**Architecture philosophy:** The plugin owns only NRC-specific presentation and derived UI state. Headlamp owns Kubernetes connectivity, cluster context, navigation conventions, authentication, RBAC behavior, and shared UI primitives. No standalone backend or custom API client is required.

**Migration strategy:** `NodeReadinessRule` remains the source for rule-level aggregate information during the initial implementation. `NodeReadinessEvaluation` becomes the source for per-node evaluation details once the NRE API is available.

```text
Headlamp
 │
 ├── Native Kubernetes resource access/ resource hooks
 │
 ├── NRC Plugin
 │    ├── Routes / Sidebar
 │    ├── NRR List + Details
 │    │     └── rule-level state
 │    │
 │    ├── NRE List + Details
 │    │     └── node-level evaluation state
 │    │
 │    └── Node Details Extension
 │          └── enrich existing Headlamp Node view
 │
 └── Headlamp shared UI / Router / theme / RBAC conventions
      ▼
Kubernetes API Server
 ├── NodeReadinessRule
 ├── NodeReadinessEvaluation
 └── Nodes
```

**Cluster Context:**
All plugin routes and Kubernetes resource queries operate against Headlamp's currently selected cluster. The plugin must not maintain cluster-global React state for NRR/NRE objects unless keyed by cluster. Route generation should use Headlamp's Router APIs so navigation remains cluster-aware.

#### Required RBAC Permissions

The plugin requires the following minimum ClusterRole permissions for full functionality:

| API Group | Resource | Verbs | Used By |
|---|---|---|---|
| `readiness.node.x-k8s.io` | `nodereadinessrules` | `get`, `list`, `watch` | Rule List, Rule Details, Landing Page |
| `readiness.node.x-k8s.io` | `nodereadinessevaluations` | `get`, `list`, `watch` | NRE List, NRE Details, Node Extension |
| `""` (core) | `nodes` | `get`, `list`, `watch` | Node Extension |

> If the user lacks NRE permissions, the NRE views show the appropriate permission error, while the Node Extension is omitted when NRC/NRE data is unavailable. If the user lacks NRR permissions, the plugin shows Headlamp's native permission error.

---

### 5. State Synchronization

The UI must stay in sync with cluster state. When an operator modifies, creates, or deletes a rule via `kubectl`, the UI must update without page reloads.

**Decision:** No custom polling or WebSockets. The plugin relies on Headlamp's native resource data/watch mechanisms rather than implementing its own polling or WebSocket transport. Changes received through Headlamp's native resource/watch mechanisms update the relevant UI state without requiring page reloads:

* **`ADDED`:** New resource appears.
* **`MODIFIED`:** Existing resource state updates.
* **`DELETED`:** Resource disappears / details view handles `404` fallback.

---

### 6. Data Fetching Strategy

#### Decisions

| Decision | Rationale |
|---|---|
| **Aggregated Landing Page.** The Landing Page performs a single API list request to populate summary metrics. It does not fetch individual Node resources or NodeReadinessEvaluations until the user navigates deeper. | Provides immediate value while deferring heavy payload loading. |
| **Initial aggregation.** The initial Landing Page may derive lightweight rule-level summary values from the NRR status already returned by Headlamp. | If the upstream `status.evaluationSummary` API becomes available, the plugin can consume those server-provided aggregates instead. |
| **Cardinality-aware filtering.** Columns like Name, Node Selector, and Taint are filterable but high-cardinality. Filters will use exact-match or substring search, not dropdown enums. Low-cardinality columns (Mode, Effect, Dry-Run) use dropdown filters. | Prevents loading thousands of unique filter values into memory. |

---

### 7. UI Surface

We define six primary user interfaces plus one optional future surface and supporting features. Status is conveyed through text and semantic status indicators, with color used as a secondary visual cue. Views follow Headlamp's existing UI patterns and use native Headlamp/MUI components where appropriate.

#### 7.1 Landing Page

The entry point for the plugin. The Landing Page provides immediate orientation for operators, acting as a lightweight summary before drilling down into the Rule List. It performs a single `NodeReadinessRule` list request.

* **CRD Detection:** Attempt the NRR resource request directly; distinguish CRD-not-installed (404) / API-not-served states from permission (403) and empty-resource states. Avoid redundant discovery calls unless Headlamp's resource API explicitly requires them. When the resource API is unavailable, show the appropriate empty/error state (e.g., a Headlamp `<EmptyContent>` component with the message: `"Node Readiness Controller CRDs not installed"`, alongside a standard Headlamp `<Link>` pointing to the [NRC installation documentation](https://node-readiness-controller.sigs.k8s.io/getting-started/)).
* **Controller Health Limitation:** CRD/API availability confirms that the resource API is available but does not guarantee that the controller is healthy or actively reconciling. Controller health detection is outside the initial plugin scope.
* **Summary Cards:** Use Headlamp's standard summary-card/layout pattern, providing high-level context without attempting complex cluster-wide unique node analytics. 
  * **Total Rules:** Count of `NodeReadinessRule` objects.
  * **Rules with Unsatisfied Nodes:** Count of rules where at least one node evaluation has `taintStatus === 'Present'`.
  * **Rules with Evaluation Errors:** Count of rules where `failedNodes` is not empty.

#### 7.2 Rule List View

Table of all `NodeReadinessRule` objects, utilizing Headlamp's native `ResourceListView`.

**Error States:**
* If the CRD is missing (API returns `404`), renders a Headlamp `<EmptyContent>` component with the message `"Node Readiness Controller CRDs not installed"` and a `<Link>` to the [installation guide](https://node-readiness-controller.sigs.k8s.io/getting-started/).
* If RBAC denies access (API returns `403`), renders Headlamp's native permission error view.

| Column | Source | Filterable | Component & Style |
|---|---|---|---|
| Name | `metadata.name` | Yes | Standard Headlamp `<Link>` to the Rule Details page. If `spec.dryRun: true`, a "Dry Run" chip/badge is rendered beneath the link. |
| Enforcement | `spec.enforcementMode` & `spec.conditionPolicy` | Yes | Two stacked chip elements. Example: `Mode: continuous`, `Policy: allOf`. |
| Status | Computed from `nodeEvaluations` | Yes | Headlamp `<StatusLabel>`. Map semantic states first: Healthy → satisfied, Terminating → terminating, etc. Then map to Headlamp visual conventions. |
| Unsatisfied Nodes | Count where `taintStatus === 'Present'` | Yes | Standard text displaying the integer count if > 0. If 0, renders a literal `"-"`. |
| Evaluation Errors | `failedNodes.length` | Yes | Standard text displaying the integer count if > 0. If 0, renders a literal `"-"`. |
| Node Selector | `spec.nodeSelector` | No | A flex row of summary chips. For `matchLabels`: `"{count} Labels"`. For `matchExpressions`: `"{count} Expressions"`. Wrapped in a tooltip revealing full JSON on hover. |
| Taint | `spec.taint` | Yes | Standard outlined chip. Text format: `{key}={value}:{effect}` when `spec.taint.value` is set, or `{key}:{effect}` when value is empty. |
| Age | `metadata.creationTimestamp` | No | Headlamp's standard relative-time formatting. |

*(Note on filtering: Columns should expose filter/sort-friendly primitive values where supported by Headlamp's table APIs, rather than dictating exact dropdowns or substring implementations in the design).*

#### 7.3 Rule Details View

Deep dive into a single `NodeReadinessRule`, utilizing Headlamp's `DetailsView`.

**Error States:**
* If navigating to a deleted rule (API returns `404`), renders Headlamp's native `<EmptyContent>` with text `"Resource not found"`.
* If RBAC denies access (API returns `403`), renders a custom `<EmptyContent>` explaining the lack of read permissions for `nodereadinessrules`.

**Basic Info Section:**
Rendered using Headlamp's standard resource details/header components.

| Field | Source | Component & Style |
|---|---|---|
| Name | `metadata.name` | Headlamp's standard header. |
| Creation Timestamp | `metadata.creationTimestamp` | Headlamp's standard header. |
| Labels | `metadata.labels` | Headlamp's standard header. |
| Node Selector | `spec.nodeSelector` | Flex container of summary chips. For `matchLabels`: one chip per key/value pair formatted as `key=value`. For `matchExpressions`: one chip per expression formatted as `key operator [values]`. |
| Enforcement Mode | `spec.enforcementMode` | Standard outlined chip (e.g., `"continuous"`). |
| Condition Policy | `spec.conditionPolicy` | Standard text (e.g., `"allOf"`). Defaults to `"allOf"` if field is absent. |
| Dry-Run | `spec.dryRun` | "Yes" warning chip if true. Standard text `"No"` if false. |
| Taint Managed | `spec.taint` | Standard text. Format: `{key}={value}:{effect}` when value is set, or `{key}:{effect}` when value is empty. |

**Node Status Section:**
A container displaying a flex row of metric chips:
  * **Rule Evaluations:** `{count}` (default chip)
  * **Satisfied:** `{count}` (success chip)
  * **Unsatisfied:** `{count}` (warning chip) - computed as count of `nodeEvaluations` where `taintStatus === 'Present'`.
  * **Evaluation Errors:** `{count}` (error chip)

If `status.nodeEvaluations` is absent, ensure the UI clearly distinguishes that evaluation is pending rather than displaying zero counts.

**Conditions Table:**
Rendered using Headlamp's shared `<SimpleTable>` component.

| Column | Source | Component & Style |
|---|---|---|
| Type | `spec.conditions[].type` | Standard text. |
| Required Status | `spec.conditions[].requiredStatus` | Standard neutral text or default chip (do not use semantic error badges for `False`). |
| Default Status | `spec.conditions[].defaultStatus` | Standard neutral text or default chip. If field is absent, displays text `"Unknown (implicit)"`. |

**Node Evaluations Table:**
Rendered using Headlamp's shared `<SimpleTable>` component. This fulfills the primary UX goal of showing operators exactly *why* a node is failing a rule.

| Column | Source | Component & Style |
|---|---|---|
| Node | `status.nodeEvaluations[].nodeName` | Headlamp `<Link>` to the node details page. |
| State | `status.nodeEvaluations[].taintStatus` | Headlamp `<StatusLabel>` (mapped from semantic state). |
| Unmatched Conditions | Computed from `conditionResults` | Renders a list of condition Types where `currentStatus != requiredStatus`. A condition-level mismatch does not necessarily imply rule failure when `conditionPolicy: anyOf`; the UI distinguishes unmatched conditions from the overall rule evaluation state. |
| Last Evaluated | `status.nodeEvaluations[].lastEvaluatedAt` | Rendered using Headlamp's standard relative-time formatting to clarify data freshness. |

**Dry-Run Results Section (Conditional):**
This section is rendered **only** when `spec.dryRun: true` and `status.dryRunResults` is present. Rendered as a standard summary card with a "Dry Run" header.

| Field | Source | Component & Style |
|---|---|---|
| Affected Nodes | `status.dryRunResults.affectedNodes` | Standard text showing the count. |
| Taints To Add | `status.dryRunResults.taintsToAdd` | Standard text showing the count. |
| Taints To Remove | `status.dryRunResults.taintsToRemove` | Standard text showing the count. |
| Risky Operations | `status.dryRunResults.riskyOperations` | Standard text showing the count. Label: `"Risky Operations"`. |
| Summary | `status.dryRunResults.summary` | Standard text block displaying the controller's human-readable summary string. |

**Events Section:**
Events are intentionally omitted from the initial implementation because they are not required for the core UI.

#### 7.4 Node Readiness Evaluation List View

Table of all `NodeReadinessEvaluation` objects, utilizing Headlamp's native `ResourceListView`. **Blocked on the NRE API becoming available in the target NRC release.**

**Error States:**
* If the NRE CRD is missing (API returns `404`), show a clear empty/error state explaining that the feature requires an NRC release providing the NRE API.
* If RBAC denies access, renders Headlamp's native permission error view.

| Column | Source | Filterable | Exact Component & Style |
|---|---|---|---|
| Name | `metadata.name` | Yes (substring) | Headlamp `<Link>` to the NRE Details page. *(Note: The current proposal uses the target node name as the NRE name; this is subject to the final API design)*. Beneath it: a smaller `"View Node"` `<Link>` pointing to Headlamp's native Node details page. |
| State | `status.state` | Yes (dropdown) | Headlamp `<StatusLabel>` using `status="success"` for `"Available"` or `status="error"` for `"Not Available"`. |
| Rules Status | Computed (Satisfied vs Total, where Total is `status.rules.length`) | Yes (dropdown: all satisfied / not all) | Headlamp `<StatusLabel>`. Uses `status="success"` for `"OK (N/N)"`, `status="warning"` for `"Not Ready (X/N)"`, or `"Evaluation Pending"` (only when evaluation data is genuinely unavailable). |
| Age | `metadata.creationTimestamp` | No | Simple `<Typography>` text rendered via Headlamp's standard relative-time/age formatting. |

#### 7.5 Node Readiness Evaluation Details View

Deep dive into a single node's evaluation. **Blocked on the NRE API becoming available in the target NRC release.**

**Error States:**
* If navigating to a deleted NRE (API returns `404`), renders Headlamp's native `<EmptyContent>` with text `"Resource not found"`.
* If RBAC denies access, renders a custom `<EmptyContent>` explaining the lack of read permissions for `nodereadinessevaluations`.

**Basic Info:**
* **Target Node:** Rendered as a `"Visit Node"` CTA `<Link>` pointing to Headlamp's native Node details page.
* **State:** Headlamp `<StatusLabel>` (`status="success"` for `"Available"`, `status="error"` for `"Not Available"`).
* **Active Taints:** If none, visually indicate absence. If present, a flex row of standard chips. Format: `{key}={value}:{effect}` when value is set, or `{key}:{effect}` when value is empty.
* **Rule Status:** A flex row of metric chips for Total, Satisfied, and Unsatisfied counts.

**Rule Evaluations Section:**
Use Headlamp's shared table components where their interaction model is sufficient. Use a custom MUI table only if expandable rule-evaluation rows cannot be represented cleanly through the existing Headlamp table APIs.

**Row Collapsed Info:**
* **Rule Name** (text), **Rule Status** (`<StatusLabel>`), **Taint** (chip), **Policy** (text).

**Row Expanded Info:**
* **Reason & Message:** Standard text fields displaying the full strings from the evaluation.
* **Timestamps (First/Last Evaluation):** Rendered using Headlamp's standard date formatting.

#### 7.6 Node Details Extension

Injected into Headlamp's native Node details page via `registerDetailsViewSection`.

**Identity:** The Node Extension derives the target Node identity from the resource passed by Headlamp rather than performing an additional Node lookup.

**Design Decision (RBAC & UX):** The extension should avoid unnecessary unauthorized NRE requests. We prefer Headlamp's existing permission/access mechanisms where available. The section is implemented as a conditional `registerDetailsViewSection` registration and returns `null` when NRC/NRE data is unavailable. If a preflight authorization check is strictly required after implementation/testing, use `SelfSubjectAccessReview` selectively rather than unconditionally on every Node details render. This prevents cluttering standard Kubernetes Node detail pages with NRC-specific error boxes for users who don't use NRC.

**Node Readiness Section:**
This section introduces a unified lifecycle-conditions panel. Standard Kubernetes conditions will be shown alongside conditions referenced by NRC rules for a holistic view of the node's health. The conditions configured in NRC's rules are read from the Node object's `status.conditions`; they are grouped separately from the standard kubelet conditions for presentation only.

*Example rendering layout:*
```text
Node Conditions
Standard Kubernetes Conditions
--------------------------------
Ready              True
MemoryPressure     False
DiskPressure       False
PIDPressure        False

Conditions Referenced by NRC Rules
--------------------------------
CNIReady           False
GPUReady           True
CSIReady           Unknown
```
* **State:** Headlamp `<StatusLabel>` (`status="success"` or `status="error"`).
* **Link:** A `"View Full Evaluation"` `<Link>` pointing to the NRE Details page.
* **Rules Table:** Headlamp `<SimpleTable>` with the following columns:
  * **Rule Name:** `<Typography>` text.
  * **Status:** `<StatusLabel>`.
  * **Taint:** Standard outlined chip.
  * **Reason:** Truncated when necessary, with access to the full message.

#### 7.7 Map View (Future Goal)

Investigate integrating NRC data into Headlamp's native Relationship Map view to visualize connections between rules and the nodes they target. This is an extended goal and will not block initial milestones.

---

### 8. Supporting Features

#### Headlamp Form (Future Phase)
*Note: Delegated to a future phase due to bandwidth constraints.*
Support creating `NodeReadinessRule` objects via Headlamp's form system.
* Selecting `nodeSelector` as `{}` (empty) should display a warning: `"This targets all nodes in the cluster."`.
* Selecting taint effect as `NoExecute` should display a warning: `"NoExecute evicts existing pods."`.
* Kubernetes RBAC remains authoritative for create/update authorization; the UI should surface permission failures clearly.

#### RBAC Handling
The built-in Headlamp `ResourceListView` already handles RBAC errors for list views (rendering a native permission error). For the Details page, a custom `<EmptyContent>` is rendered when the user lacks read permission for `nodereadinessrules` or `nodereadinessevaluations`, with a clear message explaining which permission is missing.

#### Dry-Run Mode
When `spec.dryRun: true`, the Rule Details view renders the standard Node Status Section (from `status.nodeEvaluations`) **plus** a dedicated Dry-Run Results Section (from `status.dryRunResults`) that shows the controller's simulation output. See Section 7.3 for the full rendering specification.

There is ongoing design work to evolve dry-run into a **lifecycle** - continuous per-node simulation via NRE, where each node gets a rule evaluation object with a dry-run status showing the evaluated outcome without actually applying taints. The plugin will adopt richer per-node dry-run data once the upstream API supports it.

---

### 9. Error Handling and Edge Cases

| Scenario | UI Behavior |
|---|---|
| CRD not installed (API returns `404` for `nodereadinessrules`) | Landing Page and Rule List show `<EmptyContent>` with message `"Node Readiness Controller CRDs not installed"` and a `<Link>` to installation instructions. |
| NRE CRD not installed | NRE List shows `<EmptyContent>` with message `"NodeReadinessEvaluation CRD not installed. Requires an NRC release containing the NRE CRD."`. Node Extension is silently omitted. |
| NRE CRD installed but NRE controller feature disabled | NRE views show an empty/evaluation-unavailable state rather than treating the CRD's presence as proof that NRE data is being produced. |
| RBAC denies CRD read access (`403`) | List views show Headlamp's native permission error. Details pages show a custom `<EmptyContent>` with the missing permission. |
| Rule has empty `status` (first evaluation pending) | Status column shows `"Pending"`. Node Status chips show `"-"`. No zero counts. |
| `metadata.deletionTimestamp` is present | Rule List shows `<StatusLabel>` with `status="warning"` and text `"Terminating"`. |
| `spec.dryRun: true` | Rule List shows `<Chip color="warning" />` with text `"Dry Run"`. Rule Details renders the Dry-Run Results Section with simulation data. |
| Node has no NRE evaluation data | Node Extension is silently omitted. (Omitted because the initial Node Extension uses NRE as its authoritative per-node evaluation source). |
| `spec.taint.value` is empty | Taint rendered as `{key}:{effect}` (no `=`). |
| Reason/Message when rule is already satisfied | Display whatever reason is set by the controller. If absent, show `"-"`. |
| Evaluation Errors vs Unsatisfied | These are separate concepts. Evaluation Errors occur when the controller fails to read a node's state. Unsatisfied is when the node successfully evaluates but fails the health check. |
| Rule not found on navigation (`404`) | Details page renders `<EmptyContent>` with text `"Resource not found"`. |

---

### 10. UI Maturity / Release Readiness

The following are project maturity labels, not Headlamp API stability guarantees:

* **ALPHA:** The component is tied to upstream experimental APIs (e.g., NRE) or may change layout drastically.
* **BETA:** The component layout is a freeze candidate. 
* **STABLE:** The component layout and behavior are considered stable for production use.

| Component | Initial Tier | Notes |
|---|---|---|
| Rule List | ALPHA | Core deliverable. Kept in ALPHA initially to allow rapid column iteration. |
| Rule Details | ALPHA | Core deliverable. Kept in ALPHA initially to allow layout iteration. |
| NRE List | ALPHA | Depends on the experimental NRE API and its finalization. |
| NRE Details | ALPHA | Expandable row UX needs Headlamp maintainer review. |
| Node Extension | ALPHA | Use Headlamp's documented `registerDetailsViewSection` extension point. |
| Landing Page | ALPHA | Summary aggregation may evolve as the upstream NRR status API evolves. |
| Headlamp Form | ALPHA | Delegated to a future phase. |

---

### 11. Validation and Scale

* **Concurrent Testing Strategy:** Unit tests, integration tests, and scale testing are shipped concurrently with each milestone PR, rather than batched at the end of the project.
* **Unit tests:** Vitest + React Testing Library. Mock CRD data to test column rendering, chip values, routing, and filter logic.
* **Headlamp Integration tests:** Test plugin registration, route registration, sidebar visibility, Node detail extension injection, cluster switching, dark/light theme, accessible keyboard interaction, and loading/empty/error states.
* **Cluster Integration tests:** Deploy on a local `kind` cluster with NRC installed. Verify watch updates, CRD-not-installed fallback, and RBAC rejections.
* **NRE soaking:** The plugin serves as an early consumer of the NRE CRD once it ships as experimental. Bugs and design gaps discovered during plugin development will be reported back to the NRC controller team for iteration while the NRE API remains experimental.
* **Scale limits:** The initial implementation may derive lightweight summary values from the NRR status already returned by Headlamp. Performance will be validated using representative NRR status cardinality, including API payload size, browser memory usage, and rendering behavior. When the upstream `status.evaluationSummary` API is available, the plugin can consume the server-provided aggregates instead. Cardinality-heavy filter columns (Name, Node Selector, Taint) use substring search, not enumerated dropdowns, to prevent loading thousands of unique values into memory.

---

### 12. Headlamp Ecosystem Requirements

* **Headlamp Compatibility:** The plugin targets a documented minimum validated Headlamp version. CI/build/package metadata must use the corresponding plugin runtime compatibility requirements.
* **Documentation:** The plugin repository contains its own `README.md` covering installation, supported Headlamp/NRC versions, permissions, known limitations, and development commands.
* **Internationalization (i18n):** All user-facing plugin strings will use Headlamp's supported i18n mechanism; strings should not be hard-coded directly in UI components.
* **Accessibility (a11y):** Accessibility is considered from the beginning, including keyboard navigation, semantic status text (not color-only), accessible tooltips, and table semantics. M8 serves as the final audit, not the first time a11y is considered.
* **Artifact Hub Publishing:** The publishing milestone (M9) explicitly includes: `artifacthub-repo.yml`, `artifacthub-pkg.yml`, semantic versioned release, packaged plugin checksum, Headlamp compatibility metadata, and installation documentation.
* **Theme Integration:** Use Headlamp/MUI theme tokens rather than hard-coded colors. Ensure all standard components render correctly in both light and dark themes. We will verify both themes during the polish milestone.

---

### 13. Rollout Plan

The plugin is developed and merged incrementally. Each milestone corresponds to a focused, reviewable PR. NRE-dependent views are aligned with the NRC release schedule.

| Milestone | Deliverable | Target | Dependencies |
|---|---|---|---|
| **M1: Foundation** | Scaffolding, `package.json`, routing, sidebar registration, and basic CRD detection. | Done | None |
| **M2: Core Observability** | Rule List and Rule Details views with full spec/status rendering. | Week 4 | None |
| **M3: Dashboards & Design**| Design Doc approval. Landing Page with Summary Cards and empty states. | Week 5 | M2 merged |
| **Alpha Release** | Cut `v0.1.0-alpha` release for initial maintainer/community testing. | Week 5 | M3 merged |
| **M4: NRE Integration** | NRE List and Details views. *(Pivot: if NRE is delayed upstream, push M4 and M5).* | After the NRC release containing the NRE API | NRE API merged and available in the target NRC release |
| **M5: Node Extension** | Node Details injection (NRC taint grouping, lifecycle-conditions panel). | After M4 | M4 merged |
| **M6: Rule Management** | Headlamp Form for creating/editing NRRs. RBAC permission boundaries. | Delegated | M3 merged |
| **Beta Release** | Cut `v0.5.0-beta` release (covers NRR/landing-page core; does not include NRE-dependent features). Initiate community bug-bash. | Week 7 | M3 merged |
| **M7: Topology Map (Optional)** | Visual cluster map view showing nodes and rule enforcement states. | Delegated | M3 merged |
| **M8: Polish & Accessibility**| Accessibility (a11y) audit and dark/light theme validation. | Week 8 | All committed P0/P1 features merged |
| **M9: Publish** | Stable release (all P0/P1 committed features merged and reviewed). M6 and M7 are excluded from initial stable release unless separately approved. | Week 9 | Beta feedback resolved |
