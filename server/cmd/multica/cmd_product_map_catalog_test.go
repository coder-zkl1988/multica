package main

import "testing"

func TestProductMapCatalogsAreTraceableAndUnique(t *testing.T) {
	seen := map[string]bool{}
	assertCatalog(t, "Multica", multicaProductCatalog, seen, 7, 16)
	assertCatalog(t, "院务系统", yuanwuProductCatalog, seen, 7, 25)
}

func assertCatalog(t *testing.T, product string, catalog []productMapCatalogNode, seen map[string]bool, minimumGroups, minimumLeaves int) {
	t.Helper()
	if len(catalog) < minimumGroups {
		t.Fatalf("%s catalog has %d groups, want at least %d", product, len(catalog), minimumGroups)
	}
	leaves := 0
	var walk func([]productMapCatalogNode)
	walk = func(nodes []productMapCatalogNode) {
		for _, node := range nodes {
			if node.Name == "" || node.Slug == "" || node.Description == "" {
				t.Errorf("%s catalog has incomplete node: %+v", product, node)
			}
			if seen[node.Slug] {
				t.Errorf("duplicate product-map slug %q", node.Slug)
			}
			seen[node.Slug] = true
			if len(node.SourcePaths) == 0 {
				t.Errorf("%s node %q has no code evidence", product, node.Name)
			}
			if len(node.Children) == 0 {
				leaves++
			}
			walk(node.Children)
		}
	}
	walk(catalog)
	if leaves < minimumLeaves {
		t.Fatalf("%s catalog has %d leaf capabilities, want at least %d", product, leaves, minimumLeaves)
	}
}
