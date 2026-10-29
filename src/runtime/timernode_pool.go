//go:build scheduler.threads || scheduler.cores

package runtime

// timerNodeFree holds nodes whose one-shot timer finished firing, linked
// through firingNext, so Reset can reuse them. Guarded by lockTimerNodes.
var (
	timerNodeFree  *timerNode
	timerNodeFreeN int
)

// freeTimerNode keeps tn for reuse. The caller holds the timer node lock and
// nothing else refers to tn.
func freeTimerNode(tn *timerNode) {
	if timerNodeFreeN >= 256 {
		return
	}
	*tn = timerNode{firingNext: timerNodeFree}
	timerNodeFree = tn
	timerNodeFreeN++
}

// releaseTimerNode keeps a node that Stop took off the queue.
func releaseTimerNode(tn *timerNode) {
	lockTimerNodes()
	freeTimerNode(tn)
	unlockTimerNodes()
}

// newTimerNode returns a zeroed node, reusing a freed one when it can.
func newTimerNode() *timerNode {
	lockTimerNodes()
	tn := timerNodeFree
	if tn != nil {
		timerNodeFree = tn.firingNext
		timerNodeFreeN--
		tn.firingNext = nil
	}
	unlockTimerNodes()
	if tn == nil {
		tn = new(timerNode)
	}
	return tn
}
