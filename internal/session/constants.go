package session

import "time"

const (
	navTimeout             = 60 * time.Second
	responseTimeout        = 15 * time.Minute
	pollIntervalActive     = 1 * time.Second
	pollIntervalDone       = 350 * time.Millisecond
	stablePollsDefault     = 2
	stablePollsLarge       = 4
	confirmDelayDefault    = 400 * time.Millisecond
	confirmDelayLarge      = 1500 * time.Millisecond
	textChunkSize          = 20000
	partialMinGap          = 15 * time.Second
	largeResponseThreshold = 6000
	plainTextMinLen        = 800
)

const jsAssistantNodes = `
		let nodes = document.querySelectorAll('[data-message-author-role="assistant"]');
		if (!nodes.length) nodes = document.querySelectorAll('article[data-turn="assistant"]');`

const jsIsStreaming = `
		function isStillStreaming(node) {
			if (!node) return true;
			if (node.getAttribute('data-is-streaming') === 'true') return true;
			if (node.querySelector('[data-is-streaming="true"]')) return true;
			if (node.querySelector('.result-streaming')) return true;
			return false;
		}`
