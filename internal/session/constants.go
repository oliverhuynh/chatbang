package session

import "time"

const (
	navTimeout             = 60 * time.Second
	responseTimeout        = 15 * time.Minute
	responseStartTimeout   = 12 * time.Second
	responseStallTimeout   = 90 * time.Second
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

const jsAssistantText = `
		function assistantText(node) {
			if (!node) return "";
			const clone = node.cloneNode(true);
			const controls = clone.querySelectorAll([
				'[data-testid="writing-block-header-magic-edit-controls"]',
				'[data-testid="writing-block-header-magic-edit-button"]',
				'button',
				'a[role="button"]'
			].join(','));
			for (const control of controls) control.remove();
			const cellText = cell => (cell.textContent || "")
				.replace(/\s+/g, " ")
				.trim()
				.replace(/\|/g, "\\|");
			const tableMarkdown = table => {
				const rows = [];
				for (const tr of table.querySelectorAll("tr")) {
					const cells = Array.from(tr.children)
						.filter(cell => cell.tagName === "TH" || cell.tagName === "TD")
						.map(cellText);
					if (cells.length) rows.push(cells);
				}
				if (!rows.length) return "";
				const width = Math.max(...rows.map(row => row.length));
				const pad = row => Array.from({length: width}, (_, i) => row[i] || "");
				const line = row => "| " + pad(row).join(" | ") + " |";
				return [
					line(rows[0]),
					line(Array.from({length: width}, () => "---")),
					...rows.slice(1).map(line)
				].join("\n");
			};
			for (const table of clone.querySelectorAll("table")) {
				const markdown = tableMarkdown(table);
				table.replaceWith(document.createTextNode(markdown ? "\n" + markdown + "\n" : ""));
			}
			return clone.textContent || "";
		}`

const jsIsStreaming = `
		function isStillStreaming(node) {
			if (!node) return true;
			if (node.getAttribute('data-is-streaming') === 'true') return true;
			if (node.querySelector('[data-is-streaming="true"]')) return true;
			if (node.querySelector('.result-streaming')) return true;
			return false;
		}`
