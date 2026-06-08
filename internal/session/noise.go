package session

import (
	"fmt"
	"log"
	"strings"
)

// suppressChromedpNoise hides harmless CDP events from newer Chrome versions
// that chromedp does not handle yet (e.g. EventTopLayerElementsUpdated).
func suppressChromedpNoise(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	if strings.Contains(msg, "unhandled node event") ||
		strings.Contains(msg, "unhandled page event") {
		return
	}
	log.Printf(format, a...)
}
