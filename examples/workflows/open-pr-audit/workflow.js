export const meta = {
  name: "open-pr-audit",
  description: "Audit every open PR, rule on overlapping clusters, and adversarially verify proposed dispositions",
  phases: ["audit", "rule", "verify"],
};

const dispositions = [
  "merge-ready",
  "superseded",
  "stale",
  "conflict",
  "refuted",
  "close",
  "needs-review",
];

const auditSchema = {
  type: "object",
  additionalProperties: false,
  required: ["pr", "disposition", "needsRuling", "needsVerify", "evidence", "summary"],
  properties: {
    pr: { type: "integer" },
    disposition: { type: "string", enum: dispositions },
    needsRuling: { type: "boolean" },
    needsVerify: { type: "boolean" },
    evidence: { type: "array", items: { type: "string" } },
    summary: { type: "string" },
  },
};

const rulingSchema = {
  type: "object",
  additionalProperties: false,
  required: ["cluster", "winner", "losers", "dispositions", "evidence"],
  properties: {
    cluster: { type: "string" },
    winner: { type: "integer" },
    losers: { type: "array", items: { type: "integer" } },
    dispositions: {
      type: "object",
      additionalProperties: { type: "string", enum: dispositions },
    },
    evidence: { type: "array", items: { type: "string" } },
  },
};

const verdictSchema = {
  type: "object",
  additionalProperties: false,
  required: ["pr", "proposedDisposition", "upheld", "finalDisposition", "evidence"],
  properties: {
    pr: { type: "integer" },
    proposedDisposition: { type: "string", enum: dispositions },
    upheld: { type: "boolean" },
    finalDisposition: { type: "string", enum: dispositions },
    evidence: { type: "array", items: { type: "string" } },
  },
};

const priors = {
  35: "Won a prior tournament against PRs 37 and 39.",
  37: "Lost a prior tournament against PRs 35 and 39.",
  39: "Previously appeared to supersede 35 and 37, but that conclusion was REFUTED: documentation described a return type absent from code, storage runtimes were disjoint, smoke functions were dead, and hosted deployment behavior was incomplete.",
};

function shellQuote(value) {
  return `'${String(value).replaceAll("'", `'\\''`)}'`;
}

function fileClusterKey(path) {
  const parts = String(path).split("/");
  if (parts.length > 1) return parts[0];
  return parts[0] || String(path);
}

function clusterPRs(prs) {
  const byArea = new Map();
  for (const pr of prs) {
    const areas = new Set((pr.files || []).map(file => fileClusterKey(file.path || file)));
    if (areas.size === 0) areas.add(`unique:${pr.number}`);
    for (const area of areas) {
      if (!byArea.has(area)) byArea.set(area, []);
      byArea.get(area).push(pr.number);
    }
  }

  const assigned = new Set();
  const clusters = [];
  for (const [area, numbers] of byArea) {
    const members = [...new Set(numbers)].filter(number => !assigned.has(number));
    if (members.length < 2) continue;
    members.forEach(number => assigned.add(number));
    clusters.push({ id: `files:${area}`, members });
  }
  for (const pr of prs) {
    if (!assigned.has(pr.number)) {
      clusters.push({ id: `unique:${pr.number}`, members: [pr.number] });
    }
  }
  return clusters;
}

function auditPrompt(pr, cluster, mainLog, today) {
  const prior = priors[pr.number]
    ? `\nPrior audit memory (treat as a claim to re-check, not truth):\n${priors[pr.number]}\n`
    : "";
  return `You are the read-only auditor for PR #${pr.number} in ${pr.repo}.
Today is ${today}. Origin is already fetched. Do not fetch, check out, or modify anything.

PR metadata:
- title: ${pr.title}
- branch: ${pr.branch}
- draft: ${pr.draft}
- mergeable: ${pr.mergeable}
- updatedAt: ${pr.updatedAt}
- cluster: ${cluster.id}
- changed files: ${(pr.files || []).map(file => file.path || file).join(", ")}
${prior}
Recent main history:
${mainLog}

Inspect the PR with read-only gh/git/diff/source commands. Determine a concrete disposition. Cite actual repository evidence. Set needsRuling when this PR overlaps competitors in its cluster. Set needsVerify for any consequential or uncertain merge-ready/close/superseded judgment.`;
}

function rulingPrompt(cluster, candidates, audits) {
  return `You are the read-only tournament ruler for overlapping cluster ${cluster.id}.
Origin is already fetched. Do not fetch, check out, or modify anything.

Candidates:
${candidates.map(pr => `- #${pr.number}: ${pr.title} (${pr.branch})`).join("\n")}

Independent audit outputs:
${JSON.stringify(audits, null, 2)}

Compare actual diffs and repository state. Pick the one best implementation as winner, assign dispositions to every loser, and cite evidence. Do not average the audits; resolve contradictions.`;
}

function verifyPrompt(pr, proposedDisposition, evidence) {
  return `You are an adversarial verifier in ${pr.repo}. Read only.
Origin is already fetched. Do not fetch, check out, or modify anything.

The proposed disposition for PR #${pr.number} is ${proposedDisposition}. This came from prior agents, not from you.

Try to refute it using actual repository state:
- gh pr view and diff checks
- source and tests
- git logs and recently merged work
- conflict, close, superseded, and stale heuristics

Prior evidence:
${JSON.stringify(evidence, null, 2)}

Return the final verdict only after actively searching for contradictory evidence.`;
}

export async function run(sdk) {
  const today = sdk.today();
  sdk.log("info", "pre-fetching open PR metadata");
  const base = await sdk.fetchJSON(
    "gh pr list --state open --limit 200 --json number,title,headRefName,isDraft,mergeable,updatedAt,url"
  );
  const repoResult = await sdk.runBash("gh repo view --json nameWithOwner -q .nameWithOwner");
  if (repoResult.exitCode !== 0) {
    throw new Error(`gh repo view failed: ${repoResult.stderr}`);
  }
  const repo = repoResult.stdout.trim();
  const mainLogResult = await sdk.runBash("git log --oneline -30 --decorate");
  const mainLog = mainLogResult.stdout;

  const prs = [];
  for (const raw of base) {
    const files = await sdk.fetchJSON(
      `gh pr view ${raw.number} --json files -q ${shellQuote(".files")}`
    );
    prs.push({
      number: raw.number,
      title: raw.title,
      branch: raw.headRefName,
      draft: raw.isDraft,
      mergeable: raw.mergeable,
      updatedAt: raw.updatedAt,
      url: raw.url,
      repo,
      files,
    });
  }

  const byNumber = Object.fromEntries(prs.map(pr => [pr.number, pr]));
  const clusters = clusterPRs(prs);
  const clusterByNumber = {};
  for (const cluster of clusters) {
    for (const number of cluster.members) clusterByNumber[number] = cluster;
  }

  const auditStage = async () => prs.map(pr => ({
    key: `audit:${pr.number}`,
    role: "audit",
    readonly: true,
    tools: ["read_file", "list_dir", "glob", "grep", "bash"],
    prompt: auditPrompt(pr, clusterByNumber[pr.number], mainLog, today),
    responseSchema: auditSchema,
  }));

  const ruleStage = async ({ previousResults }) => {
    const auditsByPR = Object.fromEntries(
      previousResults
        .filter(result => result.ok && result.json)
        .map(result => [result.json.pr, result.json])
    );
    return clusters
      .filter(cluster => cluster.members.length > 1)
      .map(cluster => ({
        key: `rule:${cluster.id}`,
        role: "rule",
        readonly: true,
        tools: ["read_file", "list_dir", "glob", "grep", "bash"],
        prompt: rulingPrompt(
          cluster,
          cluster.members.map(number => byNumber[number]),
          cluster.members.map(number => auditsByPR[number]).filter(Boolean)
        ),
        responseSchema: rulingSchema,
      }));
  };

  const verifyStage = async ({ results }) => {
    const proposed = new Map();
    for (const result of Object.values(results)) {
      if (!result.ok || !result.json) continue;
      if (result.key.startsWith("audit:") && result.json.needsVerify) {
        proposed.set(result.json.pr, {
          disposition: result.json.disposition,
          evidence: result.json.evidence,
        });
      }
      if (result.key.startsWith("rule:")) {
        for (const [number, disposition] of Object.entries(result.json.dispositions || {})) {
          proposed.set(Number(number), {
            disposition,
            evidence: result.json.evidence,
          });
        }
      }
    }
    return [...proposed.entries()].map(([number, proposal]) => ({
      key: `verify:${number}:${proposal.disposition}`,
      role: "verify",
      readonly: true,
      tools: ["read_file", "list_dir", "glob", "grep", "bash"],
      prompt: verifyPrompt(byNumber[number], proposal.disposition, proposal.evidence),
      responseSchema: verdictSchema,
    }));
  };

  const pipeline = await sdk.pipeline(
    { prs, byNumber, clusters, mainLog, priors },
    auditStage,
    ruleStage,
    verifyStage
  );

  const verdicts = Object.values(pipeline.results)
    .filter(result => result.key.startsWith("verify:") && result.ok)
    .map(result => result.json);
  return {
    repository: repo,
    audited: prs.length,
    clusters: clusters.length,
    verdicts,
    pipeline,
  };
}
