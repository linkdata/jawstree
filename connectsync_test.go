package jawstree

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linkdata/deadlock"
	"github.com/linkdata/jaws"
)

// TestTree_DirtyReachesRequestPendingDuringChange checks that a selection
// changed after rendering reaches a Request when its WebSocket connects.
func TestTree_DirtyReachesRequestPendingDuringChange(t *testing.T) {
	jw, err := jaws.New()
	maybeError(t, err)
	defer jw.Close()
	go jw.Serve()

	rootnode := &Node{Name: "root", Children: []*Node{{Name: "child"}}}
	var mu deadlock.RWMutex
	tree := mustNew(t, &mu, rootnode)

	// Render the request while it is PENDING. JawsRender registers the dirty tag on
	// the element and folds the render-time selection (empty) into the initializer.
	httpRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	rq := jw.NewRequest(httptest.NewRecorder(), httpRequest)
	elem := rq.NewElement(tree)
	var body bytes.Buffer
	maybeError(t, elem.JawsRender(&body, nil))

	// Select the child and dirty while the request is still pending, i.e. entirely
	// inside the render→subscribe window. The child is preorder index 1.
	maybeError(t, tree.SetSelected([][]string{{"child"}}))
	tree.Dirty(jw)

	// Only now connect the request, starting its processing loop.
	if claimed := jw.UseRequest(rq.JawsKey, httpRequest); claimed != rq {
		t.Fatal("failed to claim rendered request")
	}
	inCh, outCh, _, readyCh, doneCh := jw.TestServe(rq, func(recovered any) {
		if recovered != nil {
			panic(recovered)
		}
	})
	defer func() {
		close(inCh)
		<-doneCh
	}()
	<-readyCh

	// The first update pass must carry the current (mutated) selection: the child is
	// preorder index 1. Decode the payload so this is agnostic to the sparse/bitmap
	// wire form.
	msgs := collectSelectionMessages(t, outCh)
	found := false
	for _, msg := range msgs {
		idx := decodeSelectionPayload(t, strings.TrimPrefix(msg.Data, "jawstreeSelection="), len(tree.byIndex))
		if len(idx) == 1 && idx[0] == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("request pending during the change did not receive current selection; got %#v", msgs)
	}
}
