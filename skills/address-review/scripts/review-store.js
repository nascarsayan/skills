'use strict';

/**
 * Review threads for one worktree: a local, pre-publication PR discussion
 * between a human and one or more agents.
 *
 * Deliberately dependency-free and free of any `vscode` import, so the same
 * code backs the extension and the `bin/review` CLI that agents drive.
 *
 * Store lives at <worktree>/.worktree-review/threads.json and is excluded via
 * .git/info/exclude, so the discussion never lands in a commit.
 */

const fs = require('fs');
const path = require('path');

const STORE_DIR = '.worktree-review';
const STORE_FILE = 'threads.json';
const DISCUSSION_FILE = 'DISCUSSION.md';
const VERSION = 1;

/** Who may set what: agents report on their own work, the human signs off. */
const STATUS = {
	open: { label: 'open', resolved: false, byAgent: true },
	addressed: { label: 'addressed', resolved: false, byAgent: true },
	rejected: { label: 'rejected', resolved: false, byAgent: true },
	noted: { label: 'noted', resolved: false, byAgent: true },
	resolved: { label: 'resolved', resolved: true, byAgent: false },
};
const STATUSES = Object.keys(STATUS);

/** What an agent did about a comment, shown as a badge on its reply. */
const ACTIONS = ['accepted', 'rejected', 'noted', 'question', 'review'];

function storeDir(worktree) {
	return path.join(worktree, STORE_DIR);
}
function storePath(worktree) {
	return path.join(storeDir(worktree), STORE_FILE);
}
function discussionPath(worktree) {
	return path.join(storeDir(worktree), DISCUSSION_FILE);
}

function emptyStore() {
	return { version: VERSION, threads: [] };
}

function parseStore(raw) {
	const parsed = JSON.parse(raw);
	if (!parsed || !Array.isArray(parsed.threads)) return emptyStore();
	parsed.version = parsed.version || VERSION;
	return parsed;
}

function load(worktree) {
	try {
		return parseStore(fs.readFileSync(storePath(worktree), 'utf8'));
	} catch (e) {
		if (e.code !== 'ENOENT') throw new Error(`unreadable review store at ${storePath(worktree)}: ${e.message}`);
		return emptyStore();
	}
}

async function loadAsync(worktree) {
	try {
		return parseStore(await fs.promises.readFile(storePath(worktree), 'utf8'));
	} catch (e) {
		if (e.code !== 'ENOENT') throw new Error(`unreadable review store at ${storePath(worktree)}: ${e.message}`);
		return emptyStore();
	}
}

function save(worktree, store) {
	fs.mkdirSync(storeDir(worktree), { recursive: true });
	fs.writeFileSync(storePath(worktree), JSON.stringify(store, null, 2) + '\n');
	ensureLocalIgnore(worktree);
	return store;
}

/**
 * Exclude the store through .git/info/exclude rather than .gitignore: the
 * discussion is local to this worktree and must not change the repo's tracked
 * files. Worktrees have a .git *file* pointing at the real git dir.
 */
function ensureLocalIgnore(worktree) {
	try {
		const dotGit = path.join(worktree, '.git');
		const stat = fs.statSync(dotGit);
		let gitDir = dotGit;
		if (stat.isFile()) {
			const m = /^gitdir:\s*(.+)$/m.exec(fs.readFileSync(dotGit, 'utf8'));
			if (!m) return false;
			gitDir = path.resolve(worktree, m[1].trim());
		}
		const infoDir = path.join(gitDir, 'info');
		const excludeFile = path.join(infoDir, 'exclude');
		const line = `/${STORE_DIR}/`;
		const existing = fs.existsSync(excludeFile) ? fs.readFileSync(excludeFile, 'utf8') : '';
		if (existing.split('\n').some((l) => l.trim() === line)) return false;
		fs.mkdirSync(infoDir, { recursive: true });
		fs.appendFileSync(excludeFile, `${existing && !existing.endsWith('\n') ? '\n' : ''}${line}\n`);
		return true;
	} catch (e) {
		return false;
	}
}

function nextId(store) {
	const used = new Set(store.threads.map((t) => t.id));
	for (let i = 1; ; i++) {
		const id = `t${i}`;
		if (!used.has(id)) return id;
	}
}

function findThread(store, id) {
	const thread = store.threads.find((t) => t.id === id);
	if (!thread) throw new Error(`no thread ${id} (have: ${store.threads.map((t) => t.id).join(', ') || 'none'})`);
	return thread;
}

function normalizeAuthor(author) {
	const a = author || {};
	const kind = a.kind === 'bot' ? 'bot' : 'human';
	return {
		name: a.name || (kind === 'bot' ? 'agent' : 'reviewer'),
		kind,
		...(a.agent ? { agent: a.agent } : {}),
		...(a.model ? { model: a.model } : {}),
	};
}

function addThread(store, { file, startLine, endLine, body, author, anchorText, status, action, now }) {
	if (!file) throw new Error('addThread needs a file');
	const stamp = now || new Date().toISOString();
	const thread = {
		id: nextId(store),
		file,
		startLine: Math.max(1, startLine || 1),
		endLine: Math.max(1, endLine || startLine || 1),
		anchorText: anchorText || '',
		status: status && STATUS[status] ? status : 'open',
		createdAt: stamp,
		updatedAt: stamp,
		comments: [],
	};
	store.threads.push(thread);
	addComment(store, thread.id, { body, author, action, now: stamp });
	return thread;
}

function addComment(store, threadId, { body, author, action, now }) {
	const thread = findThread(store, threadId);
	if (!body || !String(body).trim()) throw new Error('a comment needs a body');
	const stamp = now || new Date().toISOString();
	const comment = {
		id: `${thread.id}.c${thread.comments.length + 1}`,
		author: normalizeAuthor(author),
		body: String(body).trim(),
		createdAt: stamp,
		...(action && ACTIONS.includes(action) ? { action } : {}),
	};
	thread.comments.push(comment);
	thread.updatedAt = stamp;
	return comment;
}

function setStatus(store, threadId, status, { byAgent, now } = {}) {
	if (!STATUS[status]) throw new Error(`unknown status "${status}" (want one of ${STATUSES.join(', ')})`);
	if (byAgent && !STATUS[status].byAgent) throw new Error(`an agent may not set "${status}" — that is the reviewer's call`);
	const thread = findThread(store, threadId);
	thread.status = status;
	thread.updatedAt = now || new Date().toISOString();
	return thread;
}

function isOpen(thread) {
	return !STATUS[thread.status] || !STATUS[thread.status].resolved;
}

/** Threads an agent still has to answer: nothing from a bot after the last human comment. */
function needsReply(thread) {
	if (!isOpen(thread)) return false;
	const last = thread.comments[thread.comments.length - 1];
	return !!last && last.author.kind !== 'bot';
}

function counts(store) {
	const out = { threads: store.threads.length, open: 0, resolved: 0, awaitingAgent: 0 };
	for (const t of store.threads) {
		if (isOpen(t)) out.open++;
		else out.resolved++;
		if (needsReply(t)) out.awaitingAgent++;
	}
	return out;
}

/**
 * Re-anchor a thread whose line moved, by looking for its anchor text nearby.
 * Returns the 1-based line, and whether it drifted.
 */
function reanchor(thread, lines, window = 60) {
	const wanted = (thread.anchorText || '').trim();
	const at = (n) => (lines[n - 1] === undefined ? undefined : lines[n - 1].trim());
	if (!wanted) return { startLine: thread.startLine, drifted: false };
	if (at(thread.startLine) === wanted) return { startLine: thread.startLine, drifted: false };
	for (let d = 1; d <= window; d++) {
		for (const line of [thread.startLine - d, thread.startLine + d]) {
			if (line >= 1 && at(line) === wanted) return { startLine: line, drifted: true };
		}
	}
	return { startLine: thread.startLine, drifted: true, lost: true };
}

function who(author) {
	if (author.kind !== 'bot') return author.name;
	// the name is often the model itself, so do not print it twice
	const detail = [author.agent, author.model === author.name ? undefined : author.model].filter(Boolean).join(' · ');
	return `${author.name} _(${detail || 'bot'})_`;
}

function when(iso) {
	return typeof iso === 'string' ? iso.replace('T', ' ').replace(/\..*$/, ' UTC') : '';
}

/** The whole discussion as markdown, in PR-review order. */
function render(store, { branch, base, worktree } = {}) {
	const c = counts(store);
	const head = [
		`# Review — ${branch || path.basename(worktree || '.')}`,
		'',
		`${base ? `Base \`${base}\` · ` : ''}${c.threads} thread${c.threads === 1 ? '' : 's'} · ${c.open} open · ${c.resolved} resolved${
			c.awaitingAgent ? ` · ${c.awaitingAgent} awaiting an agent` : ''
		}`,
		'',
		'_Generated by Git Worktree Diff. Edit comments in the editor or through `bin/review`; this file is overwritten._',
		'',
	];

	const openItems = store.threads.filter((t) => isOpen(t));
	if (openItems.length) {
		head.push('## Open items', '');
		for (const t of openItems) {
			const last = t.comments[t.comments.length - 1];
			head.push(`- [ ] **${t.id}** \`${t.file}:${t.startLine}\` — ${t.status}${needsReply(t) ? ' · awaiting an agent' : ''}: ${firstLine(last)}`);
		}
		head.push('');
	}

	const body = [];
	const byFile = new Map();
	for (const t of store.threads) {
		if (!byFile.has(t.file)) byFile.set(t.file, []);
		byFile.get(t.file).push(t);
	}
	for (const [file, threads] of [...byFile.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
		body.push(`## \`${file}\``, '');
		for (const t of threads.sort((a, b) => a.startLine - b.startLine)) {
			const lines = t.endLine > t.startLine ? `${t.startLine}-${t.endLine}` : `${t.startLine}`;
			body.push(`### ${t.id} · line ${lines} · **${t.status}**`, '');
			if (t.anchorText) body.push('```', t.anchorText, '```', '');
			for (const comment of t.comments) {
				const badge = comment.action ? ` · \`${comment.action}\`` : '';
				body.push(`**${who(comment.author)}** · ${when(comment.createdAt)}${badge}`, '');
				body.push(
					comment.body
						.split('\n')
						.map((l) => `> ${l}`)
						.join('\n'),
					''
				);
			}
			body.push('---', '');
		}
	}

	if (!store.threads.length) body.push('_No comments yet. Select code in a worktree file and add a review comment._', '');
	return head.concat(body).join('\n');
}

function firstLine(comment) {
	if (!comment) return '(no comment)';
	const line = comment.body.split('\n').find((l) => l.trim());
	return (line || '').slice(0, 120);
}

module.exports = {
	STORE_DIR,
	STORE_FILE,
	DISCUSSION_FILE,
	STATUS,
	STATUSES,
	ACTIONS,
	storeDir,
	storePath,
	discussionPath,
	emptyStore,
	load,
	loadAsync,
	save,
	ensureLocalIgnore,
	findThread,
	addThread,
	addComment,
	setStatus,
	isOpen,
	needsReply,
	counts,
	reanchor,
	render,
	normalizeAuthor,
};
