#!/usr/bin/env node
'use strict';

/**
 * Agent-facing CLI over the local review store. Used by the `worktree-review-vscode`
 * skill so an agent never hand-edits threads.json.
 *
 *   review list [--open|--awaiting|--all] [--json]
 *   review show <id> [--json]
 *   review reply <id> --body <text|-> [--action accepted|rejected|noted|question]
 *                     [--author <name>] [--agent <tool>] [--model <id>] [--human]
 *   review add --file <path> --line <n> [--end <n>] --body <text|-> [identity flags]
 *   review status <id> <open|addressed|rejected|noted|resolved>
 *   review render [--stdout]
 *   review counts [--json]
 *
 * Global: --dir <worktree>   (default: the worktree containing the cwd)
 * `--body -` reads the body from stdin, which is the safe way to pass markdown.
 */

const fs = require('fs');
const path = require('path');
const cp = require('child_process');
const store = require('./review-store.js');

const HELP = `review — local pre-publication PR discussion for a git worktree

  review list [--open|--awaiting|--all] [--json]
  review show <id> [--json]
  review reply <id> --body <text|-> [--action accepted|rejected|noted|question]
                    [--author <name>] [--agent <tool>] [--model <id>] [--human]
                    [--status addressed|rejected|noted]
  review add --file <path> --line <n> [--end <n>] --body <text|-> [identity flags]
  review status <id> <${store.STATUSES.join('|')}>
  review render [--stdout]
  review counts [--json]

Global: --dir <worktree>   default: the worktree containing the cwd
"--body -" reads the body from stdin, which is the safe way to pass markdown.
Only the reviewer may set "resolved"; agents use addressed / rejected / noted.
`;

function die(msg, code = 1) {
	process.stderr.write(`review: ${msg}\n`);
	process.exit(code);
}

function parseArgs(argv) {
	const out = { _: [], flags: {} };
	for (let i = 0; i < argv.length; i++) {
		const a = argv[i];
		if (a.startsWith('--')) {
			const key = a.slice(2);
			const next = argv[i + 1];
			if (next === undefined || next.startsWith('--')) out.flags[key] = true;
			else out.flags[key] = argv[++i];
		} else out._.push(a);
	}
	return out;
}

/** Top level of the worktree containing `from`. */
function worktreeRoot(from) {
	try {
		return cp.execFileSync('git', ['-C', from, 'rev-parse', '--show-toplevel'], { encoding: 'utf8' }).trim();
	} catch (e) {
		die(`${from} is not inside a git worktree`);
	}
}

function readBody(value) {
	if (value === '-' || value === true) return fs.readFileSync(0, 'utf8').trim();
	return typeof value === 'string' ? value : '';
}

function identity(flags) {
	if (flags.human) return { name: flags.author || process.env.USER || 'reviewer', kind: 'human' };
	return {
		name: flags.author || flags.model || 'agent',
		kind: 'bot',
		...(flags.agent ? { agent: flags.agent } : {}),
		...(flags.model ? { model: flags.model } : {}),
	};
}

function branchOf(worktree) {
	try {
		return cp.execFileSync('git', ['-C', worktree, 'rev-parse', '--abbrev-ref', 'HEAD'], { encoding: 'utf8' }).trim();
	} catch (e) {
		return path.basename(worktree);
	}
}

function anchorFor(worktree, file, line) {
	try {
		const lines = fs.readFileSync(path.join(worktree, file), 'utf8').split('\n');
		return (lines[line - 1] || '').trim();
	} catch (e) {
		return '';
	}
}

function threadSummary(t) {
	const last = t.comments[t.comments.length - 1];
	return {
		id: t.id,
		file: t.file,
		startLine: t.startLine,
		endLine: t.endLine,
		status: t.status,
		awaitingAgent: store.needsReply(t),
		comments: t.comments.length,
		lastAuthor: last ? last.author.name : undefined,
		lastBody: last ? last.body : undefined,
	};
}

function printThread(t) {
	const lines = t.endLine > t.startLine ? `${t.startLine}-${t.endLine}` : `${t.startLine}`;
	process.stdout.write(`\n${t.id}  ${t.file}:${lines}  [${t.status}]${store.needsReply(t) ? '  <- awaiting an agent' : ''}\n`);
	if (t.anchorText) process.stdout.write(`     | ${t.anchorText}\n`);
	for (const c of t.comments) {
		const tag = c.author.kind === 'bot' ? `${c.author.name}${c.author.agent ? `/${c.author.agent}` : ''}` : c.author.name;
		process.stdout.write(`     ${tag}${c.action ? ` (${c.action})` : ''}: ${c.body.split('\n').join('\n       ')}\n`);
	}
}

function main(argv) {
	const { _: positional, flags } = parseArgs(argv);
	const command = positional[0];
	if (!command || flags.help || command === 'help') {
		process.stdout.write(HELP);
		return;
	}

	const worktree = flags.dir ? path.resolve(String(flags.dir)) : worktreeRoot(process.cwd());
	const data = store.load(worktree);

	switch (command) {
		case 'list': {
			let threads = data.threads;
			if (flags.awaiting) threads = threads.filter(store.needsReply);
			else if (!flags.all) threads = threads.filter(store.isOpen);
			if (flags.json) {
				process.stdout.write(JSON.stringify({ worktree, branch: branchOf(worktree), threads: threads.map(threadSummary) }, null, 2) + '\n');
			} else if (!threads.length) {
				process.stdout.write('no matching threads\n');
			} else {
				threads.forEach(printThread);
			}
			return;
		}
		case 'show': {
			const t = store.findThread(data, positional[1] || die('show needs a thread id'));
			if (flags.json) process.stdout.write(JSON.stringify(t, null, 2) + '\n');
			else printThread(t);
			return;
		}
		case 'reply': {
			const id = positional[1] || die('reply needs a thread id');
			const body = readBody(flags.body) || die('reply needs --body <text|->');
			const comment = store.addComment(data, id, { body, author: identity(flags), action: flags.action });
			if (flags.status) store.setStatus(data, id, String(flags.status), { byAgent: !flags.human });
			store.save(worktree, data);
			writeDiscussion(worktree, data);
			process.stdout.write(`replied on ${id} as ${comment.author.name}${flags.status ? `, status ${flags.status}` : ''}\n`);
			return;
		}
		case 'add': {
			const file = flags.file || die('add needs --file <path relative to the worktree>');
			const startLine = Number(flags.line || die('add needs --line <n>'));
			const body = readBody(flags.body) || die('add needs --body <text|->');
			const thread = store.addThread(data, {
				file: String(file),
				startLine,
				endLine: Number(flags.end || startLine),
				body,
				author: identity(flags),
				action: flags.action || (flags.human ? undefined : 'review'),
				anchorText: anchorFor(worktree, String(file), startLine),
			});
			store.save(worktree, data);
			writeDiscussion(worktree, data);
			process.stdout.write(`${thread.id} ${thread.file}:${thread.startLine}\n`);
			return;
		}
		case 'status': {
			const id = positional[1] || die('status needs a thread id');
			const next = positional[2] || die(`status needs one of ${store.STATUSES.join(', ')}`);
			try {
				store.setStatus(data, id, next, { byAgent: !flags.human });
			} catch (e) {
				die(e.message);
			}
			store.save(worktree, data);
			writeDiscussion(worktree, data);
			process.stdout.write(`${id} -> ${next}\n`);
			return;
		}
		case 'render': {
			const md = store.render(data, { branch: branchOf(worktree), worktree });
			if (flags.stdout) process.stdout.write(md);
			else {
				writeDiscussion(worktree, data);
				process.stdout.write(`${store.discussionPath(worktree)}\n`);
			}
			return;
		}
		case 'counts': {
			const c = store.counts(data);
			process.stdout.write(flags.json ? JSON.stringify(c) + '\n' : `${c.threads} threads, ${c.open} open, ${c.awaitingAgent} awaiting an agent\n`);
			return;
		}
		default:
			die(`unknown command "${command}" (try: list, show, reply, add, status, render, counts)`);
	}
}

function writeDiscussion(worktree, data) {
	fs.mkdirSync(store.storeDir(worktree), { recursive: true });
	fs.writeFileSync(store.discussionPath(worktree), store.render(data, { branch: branchOf(worktree), worktree }));
}

try {
	main(process.argv.slice(2));
} catch (e) {
	die(e.message);
}
