# Distributed graph linker

Run from the project root with Go 1.23 or later:

```powershell
go run ./cmd/meshwork discover ./examples/distributed/primitives ./examples/distributed/composites
go run ./cmd/meshwork link ./examples/distributed/primitives ./examples/distributed/composites
go run ./cmd/meshwork validate ./examples/distributed/primitives ./examples/distributed/composites
go test ./...
```

Validation reports `42 places, 23 transitions, 17 scenarios`. This includes the reusable NAND, duplication, NOT, OR, and MUX definitions. OR itself has nine places and five transitions; MUX has 23 places and 14 transitions. Tests execute the four-case NAND/OR truth tables, all eight MUX cases, and the singular NAND example using the existing Go runtime.

MUX selects A when S is false and B when S is true: `Q = (A AND NOT S) OR (B AND S)`. The [MUX fixture](../examples/distributed/composites/logic/graph.mux.meshwork.yaml) reuses OR, NOT, NAND and explicit selector duplication. Each AND branch is NAND followed by NOT. Only NAND and duplication require implementation functions; MUX adds no special evaluator. Tests also round-trip its executable graph through JSON and check that only Q retains a token.

`TestIndependentDiscoveryUnifies` discovers two repositories separately and passes their combined file lists to `LinkFiles`: compatible declarations share identities and duplicate arcs collapse. Type, implementation, and node-kind conflicts return an empty result with deterministic diagnostics naming both sources, leaving source bytes unchanged. CLI tests require both `link` and `validate` to emit no output when linking fails.

For a single repository, use `meshwork discover ./workspace`, `meshwork link ./workspace`, or `meshwork validate ./workspace` after building with `go build -o meshwork.exe ./cmd/meshwork`. `link` emits canonical `meshwork.ExecutableGraph` JSON; `validate` checks structure and scenario references without executing implementation code. It cannot check whether an implementation key has a registered function. All commands return a nonzero exit status on errors.

## Discovery and repository context

Only these filename forms are discovered, recursively:

- `graph.<name>.meshwork.yaml`
- `<purpose>.scenario.<name>.meshwork.yaml` (one scenario)
- `<purpose>.scenarios.<name>.meshwork.yaml` (a nonempty collection)

The name and purpose each occupy one filename component. Other files are ignored; `.git` and `.cache` directories are skipped. Directory symlinks are not followed; matching source-file symlinks are rejected. Paths are sorted before parsing. Every source is collected before resolving references. Duplicate YAML keys, unknown fields, multiple YAML documents, and singular/plural schema mismatches are errors.

Each configured root is one repository. Positional roots use the directory basename as the repository name. Names must be unique. To assign names and a parent namespace explicitly, put flags before positional roots:

```powershell
go run ./cmd/meshwork validate -namespace /projects/logic -repo primitives=./examples/distributed/primitives -repo composites=./examples/distributed/composites
```

Without a file namespace, these repositories contribute under `/projects/logic/primitives` and `/projects/logic/composites`. A file's absolute `namespace` deliberately overrides this context. A relative namespace extends it. Directories never contribute computational path components.

## Contributions and identity

```yaml
# graph.inputs.meshwork.yaml
namespace: /logic/nand
places: {A: Bool, B: Bool, Q: Bool}
```

```yaml
# graph.nand.meshwork.yaml, possibly in another directory
namespace: /logic/nand
transitions:
  nand:
    implementation: logic.nand.go
arcs:
  - [A, nand]
  - [B, nand]
  - [nand, Q]
```

These files jointly define three places, one transition, and three arcs. Places, transitions, and arcs may each be supplied independently; executable validation happens after merging. There is no required graph ID. Matching resolved names merge, independent of the source's repository. Identical place types and transition implementations merge; arcs form a deduplicated union. Conflicting types, implementations, node kinds, or port mappings fail. Both endpoints must exist and have opposite node kinds. Conflicts include resolved names, conflicting values, and source filenames/lines; independent merge conflicts are reported together.

The consuming project owns the adjacent `logic.nand.go` source file and binds it explicitly in its application registry. `implementation` is that registry's stable key, conventionally the neighboring filename; it is not a path Meshwork opens or a package Meshwork imports. The distributed example keeps [the graph](../examples/distributed/primitives/implementation/graph.nand.meshwork.yaml), [implementation](../examples/distributed/primitives/implementation/logic.nand.go), and [consumer registry](../examples/distributed/implementations.go) separate from the Meshwork runtime.

Names can be local (`A`), relative (`../shared/A`), or absolute (`/shared/A`). Relative dot segments are normalized and cannot escape the computational root. Empty segments, trailing separators, whitespace, and backslashes are rejected. File-level bindings are explicit, simultaneous aliases; they do not chain:

```yaml
namespace: /consumer
bindings: {input: /shared/A}
places: {input: Bool}
```

This declares `/shared/A`, not `/consumer/input`.

## Definition references and explicit reuse

A graph file's handle is `repository:filename-name`, for example `primitives:nand`. Files in the same namespace are one region, so their handles select the same merged definition. Repeating a handle in different namespaces is an ambiguity error. A graph selector resolves a repository-qualified handle, a local filename handle, or a relative/absolute computational namespace. Dotted names such as `logic.nand` also select `/logic/nand` when no local handle or literal relative namespace matches.

```yaml
namespace: /logic/use
references:
  - graph: primitives:nand
    namespace: /logic/use/first
    bindings: {A: /inputs/A, B: /inputs/B, Q: /outputs/first}
  - graph: primitives:nand
    namespace: /logic/use/second
    bindings: {A: /inputs/C, B: /inputs/D, Q: /outputs/second}
```

The `namespace` on each reference is an explicit relocation of names under the target region. Bindings override individual names (including transition names); keys resolve in the target region, values in the referring file. Types are preserved. Unbound names outside the target region retain their absolute identities. Bindings to unknown objects fail. Equal final names share computational objects; unequal names remain independent. References without a namespace or bindings retain the referenced identities and create no new scope. Recursive definition expansion is rejected.

References and bindings are metadata, never arcs. The canonical workspace graph contains discovered definitions and their explicitly expanded occurrences. `Result.Definitions` exposes each selected region, including referenced definitions and the places required by selected transitions. It performs no reachability-based subgraph extraction. The [OR fixture](../examples/distributed/composites/logic/graph.or.meshwork.yaml) reuses NAND and explicit duplication with the same mechanism across repositories.

Runtime port names are the basenames of resolved arc place endpoints **before reference relocation/bindings**; file aliases are already resolved. Ports within each input/output direction must be unique. They remain attached to their endpoints when names are rebound, so a NAND function continues receiving `A`, `B`, and producing `Q` even when those endpoints resolve to other names. Transition implementations use local port names; graphs are the only reusable definitions in both YAML and Go.

## Scenarios

```yaml
# tests.scenarios.nand.meshwork.yaml
graph: primitives:nand
scenarios:
  - name: true_true
    marking: {A: true, B: true}
    expect: {Q: false}
```

The singular form uses top-level `graph`, `name`, `marking`, and optional `expect`, with no `scenarios` field. Scenario names must be nonempty and unique within their file. Place names in markings and expectations resolve in the selected graph's namespace, with optional file `bindings`. Every place must belong to that graph. Duplicate resolved marking/expectation keys fail. Values become immutable tokens with the declared nominal type; Meshwork does not infer conversions or validate value schemas.

Scenarios can live in any configured repository. They never become reusable graph declarations. Execute them with an explicit implementation registry:

```go
linked, err := linker.Link([]linker.Repository{
    {Name: "primitives", Root: "examples/distributed/primitives"},
    {Name: "composites", Root: "examples/distributed/composites"},
})
if err != nil { return err }
implementations := distributed.Implementations()
for _, scenario := range linked.Scenarios {
    _, err := linker.RunScenario(ctx, linked.Definitions[scenario.Graph], scenario, implementations)
    if err != nil { return err }
}
```

`RunScenario` instantiates a fresh runtime each time and fires each transition once, retrying disabled transitions after progress. Stalls, implementation errors, cancellation, and failed expectations return errors. This bounded helper supports these finite examples; it does not define general cyclic, reactive, concurrent, or lifecycle scheduling. Kernel atomicity and token validation remain unchanged.

Canonical output sorts objects, arcs/ports, and provenance. Shuffling discovery order produces identical output. Scenarios cannot substitute implementations or rewrite composition. Place-based subgraph traversal remains open.

## GraphML and yEd

GraphML is an input adapter to the same resolver, contribution merger, topology validator and `meshwork.ExecutableGraph` used by YAML. No implementation code runs during import. Default YAML discovery ignores `.graphml`; opt in with configured roots:

```powershell
go run ./cmd/meshwork import ./linker/testdata/graphml/graph.nand.graphml
go run ./cmd/meshwork validate ./linker/testdata/graphml/graph.nand.graphml
go run ./cmd/meshwork import ./linker/testdata/graphml/graph.nand.graphml --format yaml
go run ./cmd/meshwork import ./linker/testdata/graphml/graph.nand.graphml --format json
go run ./cmd/meshwork link ./linker/testdata/graphml --graphml-root ./linker/testdata/graphml
```

`--graphml-root` is repeatable and enables recursive GraphML discovery only inside those roots. YAML discovery retains its existing filename rules. When a GraphML root matches a configured repository root it inherits that repository's name/context; otherwise its directory basename is the repository name. Go callers combine `Discover(repos)` and `DiscoverGraphML(graphMLRepos, conventions)` results in one `LinkFiles(files)` call. `ImportGraphML(filename, conventions)` is a single-file convenience function with default context `/import`. Prefer an explicit absolute graph namespace for portable files. GraphML handles use the full filename stem, e.g. `repository:graph.nand`.

In yEd's Custom Properties dialog create string properties on nodes:

| Property | Meaning |
| --- | --- |
| `meshwork.kind` | `place`, `transition`, `annotation`, or `group` |
| `meshwork.name` | Computational name, independent of XML ID/visible label |
| `meshwork.namespace` | Optional node namespace override |
| `meshwork.type` | Nominal place token type, e.g. `Bool` |
| `meshwork.implementation` | Transition implementation |
| `meshwork.timeout` | Nonnegative transition duration such as `30s`; omitted or `infinite` means no deadline |

The GraphML `<key>` must declare `attr.name` and `attr.type="string"`, with `for="node"` (or `all`). A graph-level `meshwork.namespace` key uses `for="graph"`. Standard key defaults are supported. For example:

```xml
<key id="kind" for="node" attr.name="meshwork.kind" attr.type="string"/>
<key id="name" for="node" attr.name="meshwork.name" attr.type="string"/>
<key id="type" for="node" attr.name="meshwork.type" attr.type="string"/>
<!-- inside a graph with an explicitly declared /logic/nand namespace -->
<node id="n0">
  <data key="kind">place</data>
  <data key="name">A</data>
  <data key="type">Bool</data>
</node>
```

Nodes without a kind remain noncomputational. With `--yed` (or `conventions=true`), ellipse infers place and rectangle infers transition; a missing name on an inferred node uses its visible label. Explicit kinds always win, and explicit kinds require explicit names. Other shapes do not infer kinds. No color/geometry inference or runtime marking import occurs. Labels alone are decorative. Visual groups never become vertices or implicit namespaces; nested graph namespace properties, when explicitly present, have ordinary namespace meaning. Node namespace overrides behave like absolute-name declarations in YAML: they do not create extra graph-selection regions. A graph can span multiple namespaces and groups.

Ordinary directed edges between computational nodes become arcs; edges between decorative nodes stay decorative. An edge touching just one computational endpoint is rejected unless explicitly marked `meshwork.kind=annotation` using an edge-scoped property. Missing node references always fail, including decorative edges. Unknown Meshwork properties fail rather than silently losing intended semantics. Graph properties support namespace only; edge properties support kind only. Places cannot carry transition properties and transitions cannot carry place types.

Empty place types, missing implementation keys and absent transition inputs/outputs produce sorted `Result.Diagnostics`. Compatible known declarations can complete missing fields across files; two different nonempty types, implementations, timeouts, or node kinds halt linking with an empty Result. Structural errors (including same-kind arcs and conflicting ports) always fail. Import permits inspection of incomplete diagrams; CLI `validate` reports them and exits unsuccessfully, while CLI `link` emits no executable graph for incomplete inputs. `Instantiate` rejects empty implementations, missing types/topology. Existing nominal types have no type/schema registry; nonempty type names are not checked against an external vocabulary. Registry availability is checked at instantiation, not import.

`Result.Imports` preserves original XML, filename, node/edge properties, XML-ID-to-identity mapping, inference convention, labels, shapes, geometry and group flags. Routing, colors, fonts and other extensions remain accessible in the original XML. Presentation and provenance do not affect token hashes or semantic equality. Use `CanonicalJSON(result.Graph)` for source-independent comparison and `CanonicalYAML` for deterministic resolved YAML. Both expect linked canonical input. YAML export preserves transition timeouts and omits infinite defaults. It still refuses remapped ports it cannot preserve. Incomplete YAML exports remain incomplete and cannot pass standalone strict YAML linking.

The parser requires standard GraphML namespaces and UTF-8 XML (with optional BOM), rejects DOCTYPE/directives and unknown entities, and never resolves external resources. Limits: 8 MiB per file, 100,000 XML elements, depth 128. Hyperedges and computational GraphML ports are unsupported. ShapeNode/GroupNode/ProxyAutoBoundsNode visual content is recognized; other yEd graphics remain preserved in raw XML, with no promise of inference support. This is not full GraphML schema validation or bidirectional synchronization.

The paired fixtures in `linker/testdata/graphml/` use hand-authored yEd-compatible structures, including nested groups, geometry, colors, edge routing and a legend. They were not saved by an actual yEd installation. Tests compare NAND with YAML, execute its truth table through the existing kernel, vary presentation/IDs/XML order, merge complementary/mixed sources, verify diagnostics/provenance and reject invalid/untrusted inputs. Semantic change reporting is implemented below; XML rewriting remains out of scope.

## Semantic comparison

`meshwork diff BEFORE AFTER [--format text|json] [--yed]` loads each side independently through the existing discovery/import/link pipeline. Inputs may be GraphML files, YAML files or directories; directories discover the existing Meshwork YAML filename forms and all `.graphml` files. Diff explicitly opts these roots into GraphML discovery. A single YAML file must contain its own dependencies; use a directory for complementary files.

Each directory uses the stable repository name `workspace`, so snapshot folder names do not create computational changes. Explicit namespaces are recommended. Existing repository-qualified references must target that repository name. To compare multiple explicitly named repositories, use `Discover`/`DiscoverGraphML`, `LinkFiles` and `DiffResults` in Go with the same repository configuration on both sides. The CLI does not infer named repository boundaries from nested folders.

```go
before, err := linker.LoadComparison(beforePath, false)
// Handle err before continuing.
after, err := linker.LoadComparison(afterPath, false)
// Handle err before continuing.
report, err := linker.DiffResults(before, after)
// Handle err; json.Marshal(report) or report.WriteText(writer, beforePath, afterPath).
```

`Diff(beforeGraph, afterGraph)` accepts canonical `meshwork.ExecutableGraph` values directly. It validates absolute unique computational names, declared place endpoints, port alignment/uniqueness and nonnegative timeouts without instantiating transitions. Missing types, implementation keys and input/output topology remain comparable. Types are nominal strings; no external type registry or schema validation exists. Duplicate canonical identities are rejected; compatible source declarations must first be unified by the linker. Comparison loading permits incomplete YAML without relaxing normal YAML-only `LinkFiles` validation.

`DiffResults` additionally consumes `Result.Sources`, a sidecar mapping resolved node names and directed `from -> to` arc identities to sorted unique source-location strings. YAML locations retain file/line and GraphML locations retain file/node or edge ID, including declarations remapped by references and multiple declarations of one identity. Existing transition `Sources` provide a fallback for graphs created outside the linker. Hand-built graphs without provenance compare normally but cannot report absent source information.

Semantic matching uses resolved computational names. Places compare nominal types; transitions compare implementation and timeout. Directed arcs compare endpoints and local transition port names. A changed local port yields arc removal/addition; deleting a vertex also reports each removed arc. Transition changes use `transition_definition_changed`. At the linked stage, graph references compare their resolved target, explicit namespace and bindings; binding edits use `reference_binding_changed`, while target/namespace edits remove/add references. `Library.Link` produces this stage without flattening; `Library.Flatten` expands it. Comparing linked and flattened graphs fails explicitly. CLI comparison continues using flattened graphs. Typed before/after snapshots retain all current computational attributes. Arrays are sorted deterministically by computational identity; removals precede additions for a replaced identity or arc. Empty report categories serialize as `[]`.

Provenance changes are independent of semantic changes and include both before/after source sets. File moves, source format changes, line changes and regenerated GraphML IDs may change provenance alone. Presentation, raw XML, annotations, groups, geometry, color and document traversal order are excluded from computation. Reordering YAML can still change line provenance. Reports do not include scenarios, definition selectors or runtime markings.

Possible renames require an exact same-file GraphML node ID shared uniquely between a removed and an added identity of the same kind. Both source anchors and candidate pairs must be unambiguous, including across reference remappings. IDs in different files, regenerated IDs, ambiguous bindings and structural resemblance alone never justify a suggestion. This intentionally misses some real renames. Suggestions are evidence, not proof of author intent; removal/addition and affected arcs always remain semantic changes. No input identities or files are modified.

Ordinary comparison uses identity maps, then sorting for deterministic output; rename analysis indexes source anchors without all-pairs matching. Regression tests include a 3,000-place / 2,999-transition graph, shuffled discovery/canonical order, source multiplicity, NAND visual edits, and existing OR/MUX reuse provenance. This is a scale regression, not a performance benchmark.

Example text for identical input:

```text
Meshwork Semantic Diff

Before: graph.nand.graphml
After:  graph.nand.graphml

No semantic changes.
Presentation differences are ignored.

Provenance changes:
  None

Possible renames:
  None
```

Equivalent JSON:

```json
{
  "semanticChanges": [],
  "provenanceChanges": [],
  "possibleRenames": []
}
```

Changed elements include `kind`, `element`, `identity`, typed `before`/`after` values and `beforeSources`/`afterSources`. A type edit retains, for example, `before: {"type":"Bool"}` and `after: {"type":"Bit"}`. Successful diffs return zero even when changes exist; invalid inputs produce no partial report.

Remaining boundaries: actual yEd save/reopen remains unverified and no GraphML rewriting occurs. The next smallest useful milestone is explicit expected-failure assertions in scenarios. XML patch planning and synchronization are separate work.


## Timed execution

Transition definitions accept `timeout: 30s`; omitted, `0s` or `infinite` means no deadline. Negative, malformed and non-string timeout values fail parsing. Compatible contributions must agree on the effective timeout. GraphML uses the equivalent string `meshwork.timeout` property. Canonical JSON stores finite durations as integer nanoseconds; YAML exports finite Go duration strings and omits infinity.

Scenarios retain their existing singular/plural file schemas and marking/expectations. They add `clock: {type: manual, start: 0s}` and ordered `steps`, each containing one `advance_clock` or `expect`. A collection may supply a default clock. Step expectations accept typed values or the place states `empty`/`present`. Clocks and steps cannot add transitions/arcs, change types, replace implementation keys or substitute subgraphs. Scenario bindings only resolve marking/expectation names against the already-composed graph.

See [timing details and runnable Go API](timing.md). The standard CLI validates these declarations; the Go scenario runner executes them. A manual scenario's last step is its stopping condition, so pending timed attempts are canceled and their reservations released on return. Failures remain errors; there is no implicit retry policy or expected-failure syntax yet.
