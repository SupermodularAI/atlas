package render

import (
	"encoding/json"
	"fmt"

	"github.com/SupermodularAI/atlas/internal/model"
)

// treeNode is one node of the radial view, serialised to JSON and read by the
// inline D3 script.
//
// The shape is deliberately flat and self-describing rather than mirroring
// model.Atlas: the script does no lookups and no inference, so every claim the
// picture makes is one this file put there. A field the script would have to
// derive is a field that could be derived wrongly.
type treeNode struct {
	Name string `json:"name"`
	// Kind is the node's role in the hierarchy: "root", "source", "package"
	// or "primitive". It selects shape and colour; it is not a primitive type.
	Kind string `json:"kind"`
	// State carries the degradation level for source and package nodes:
	// "read"/"unavailable" for a source, and one of the three access values
	// for a package. Empty for root and primitive nodes.
	//
	// This is the field that keeps §7 true in the visualisation. The two
	// degradation levels reach the page as distinct values and are never
	// collapsed into one "missing" state.
	State string `json:"state,omitempty"`
	// Reason is the recorded explanation for a degraded node, verbatim from
	// atlas.json. Rendered on hover, so an operator reads why rather than
	// guessing from a colour.
	Reason string `json:"reason,omitempty"`
	// Type is the primitive type (skill, subagent, hook, command,
	// mcp_server) on primitive nodes only.
	Type string `json:"type,omitempty"`
	// Description is the harvested description, shown on hover. It is
	// third-party text: it reaches the page as a JSON string inside a
	// <script> block and is written to the DOM with textContent, never as
	// markup. See treeJSON for the escaping guarantee.
	Description string `json:"description,omitempty"`
	// Count is the number of primitives under a package node, and the number
	// of primitives under a source node. It drives node radius.
	//
	// It is a count of what Atlas harvested — not usage, popularity, or
	// endorsement. Sizing by any of those would widen the claim (§9).
	Count int `json:"count,omitempty"`
	// CardID anchors the node to the card for the same package, so clicking a
	// node scrolls to the detail the page already renders. Empty on nodes
	// that have no card (the root, and primitives, which live inside a card).
	CardID   string     `json:"cardId,omitempty"`
	Children []treeNode `json:"children,omitempty"`
}

// buildTree turns an atlas into the hierarchy the radial view draws:
// root → source → package → primitive.
//
// Every source and every package in the atlas becomes a node, including the
// degraded ones. A tree layout draws only what it is given, so a builder that
// skipped packages with no primitives would silently omit exactly the
// restricted and excluded packages Atlas exists to make visible — the failure
// §7 forbids. Those packages become leaf nodes carrying their state and
// reason instead.
func buildTree(a *model.Atlas) treeNode {
	root := treeNode{
		Name: a.Company,
		Kind: "root",
	}

	for _, src := range a.Sources {
		node := treeNode{
			Name:   src.Name,
			Kind:   "source",
			State:  src.Status,
			Reason: src.Reason,
		}

		for i := range a.Packages {
			pkg := &a.Packages[i]
			if pkg.Source != src.Name {
				continue
			}
			node.Children = append(node.Children, packageNode(pkg))
		}

		// A source's count is the primitives beneath it, so an unavailable
		// source and an empty one are both small — but they are told apart by
		// State, not by size.
		for _, child := range node.Children {
			node.Count += child.Count
		}
		root.Children = append(root.Children, node)
		root.Count += node.Count
	}

	return root
}

// packageNode builds one package node and its primitive children.
//
// The nil-vs-empty distinction in model.Package.Primitives is preserved as a
// visible difference: a harvested-but-empty package renders as a package node
// with no children, while a restricted or excluded one renders the same shape
// but carries the state and reason that say why it has none. Collapsing the
// two would erase the §5 distinction the schema exists to keep.
func packageNode(pkg *model.Package) treeNode {
	node := treeNode{
		Name:        pkg.Name,
		Kind:        "package",
		State:       pkg.Access,
		Reason:      pkg.Reason,
		Description: pkg.Description,
		CardID:      CardID(pkg.Source, pkg.Name),
	}
	for _, prim := range pkg.Primitives {
		node.Children = append(node.Children, treeNode{
			Name:        prim.Name,
			Kind:        "primitive",
			Type:        prim.Type,
			Description: prim.Description,
		})
	}
	node.Count = len(pkg.Primitives)
	return node
}

// treeJSON serialises the tree for embedding in a <script> block.
//
// Two independent guards cover the same hole, and both are load-bearing.
//
// json.Marshal escapes <, > and & to <, > and & by default
// (encoding/json's SetEscapeHTML default), so a harvested description
// containing "</script>" cannot terminate the block early. That default is
// relied on here rather than merely inherited: TestTreeJSONEscapesScriptClose
// asserts it, so a future switch to an Encoder with SetEscapeHTML(false) fails
// loudly instead of silently reopening the hole.
//
// html/template then escapes the value again for the JS context it is
// interpolated into.
//
// The script reads these strings and writes them with textContent, never as
// markup, so the escaping guard in page.gohtml remains the only one needed.
func treeJSON(a *model.Atlas) (string, error) {
	b, err := json.Marshal(buildTree(a))
	if err != nil {
		return "", fmt.Errorf("render: marshal tree: %w", err)
	}
	return string(b), nil
}
