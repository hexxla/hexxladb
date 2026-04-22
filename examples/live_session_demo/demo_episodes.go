package main

// sessionScript is built in init from episode slices — one row ≈ one discrete write / turn.
var sessionScript []sessionTurn

func init() {
	sessionScript = buildSessionScript()
}

func buildSessionScript() []sessionTurn {
	var s []sessionTurn
	s = append(s, episodeBootstrap()...)
	s = append(s, episodeWorkspacePrefs()...)
	s = append(s, episodeSecurityAndVault()...)
	s = append(s, episodeIncidentHEX991()...)
	s = append(s, episodeTicketsAndMetrics()...)
	s = append(s, episodeQuotaAndCost()...)
	s = append(s, episodeRetrievalDocs()...)
	s = append(s, episodeModerationAndMemory()...)
	s = append(s, episodeStandupAndCalendar()...)
	s = append(s, episodeClosureAndRisks()...)
	s = append(s, episodeStressLongTail()...)
	return s
}

func episodeBootstrap() []sessionTurn {
	return []sessionTurn{
		{sourceSystem, []string{tagRoleSystem, tagTopicProject},
			"You are Hexxla workspace assistant for platform engineering. Prefer cited tool results over prose." +
				" Honor deployed freeze windows. Surface seams when facts disagree.", "", ""},
		{sourceSystem, []string{tagRoleSystem, tagTopicSecurity},
			"Constraints: no production writes without approval token; redact secrets in logs; EU tenant data stays in eu-west.", "", ""},
		{sourceDeveloper, []string{tagRoleDeveloper, tagTopicProject},
			"Repo policy: Go 1.24+, conventional commits, required CHANGELOG entry for user-visible behavior.", "", ""},
		{sourceDeveloper, []string{tagRoleDeveloper, tagTopicObs},
			"Observability: RED metrics dashboard id dash/hex-platform; on-call pager rotates weekly.", "", ""},
	}
}

func episodeWorkspacePrefs() []sessionTurn {
	return []sessionTurn{
		{sourceUser, []string{tagRoleUser, tagTopicPrefs},
			"Remember: API base URL is https://api.example.com/v2 for this workspace.", "", ""},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicPrefs},
			"Acknowledged. I'll call https://api.example.com/v2 for integrations unless you redirect me.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicPrefs},
			"Also pin locale en-GB for generated summaries and date formatting.", "", ""},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicPrefs},
			"Locale pinned to en-GB for summaries and timestamps in downstream artifacts.", "", ""},
	}
}

func episodeSecurityAndVault() []sessionTurn {
	return []sessionTurn{
		{sourceUser, []string{tagRoleUser, tagTopicSecurity},
			"Did we rotate the signing key after last week's incident?", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicSecurity, tagToolMeta}, "Vault path status for signing keys.",
			"vault.read", `{"path":"secret/data/prod/signing","version_hint":14,"status":"current"}`},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicSecurity},
			"Vault reports current signing material at v14; rotation ticket HEX-901 closed 6 days ago.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicSecurity},
			"List IAM exceptions still open for break-glass.", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicSecurity, tagToolMeta}, "IAM exception queue snapshot.",
			"iam.exceptions", `{"team":"platform","states":["pending"],"count":3,"oldest_hours":92}`},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicSecurity},
			"Three pending break-glass exceptions; oldest aged 92h — I'll flag if SLA is 72h.", "", ""},
	}
}

func episodeIncidentHEX991() []sessionTurn {
	return []sessionTurn{
		{sourceUser, []string{tagRoleUser, tagTopicIncident},
			"Pager fired HEX-991 — latency spike on checkout API; what do metrics say?", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicIncident, tagToolMeta}, "Last 45m latency percentiles.",
			"metrics.query", `{"service":"checkout-api","window":"45m","p95_ms":842,"p99_ms":2103,"errors_per_min":1.8}`},
		{sourceTool, []string{tagRoleTool, tagTopicObs, tagToolMeta}, "Trace samples for checkout hot path.",
			"trace.search", `{"service":"checkout-api","span":"charge","slowest_ms":2031,"sample_id":"tr_8fd2"}`},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicIncident},
			"p95≈842ms with p99≈2.1s; error rate modest. Hot span is charge; trace tr_8fd2 shows gateway wait.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicIncident},
			"Any recent deploys or flag flips correlated?", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicIncident, tagToolMeta}, "Deploy and feature flag timeline.",
			"deploy.timeline", `{"service":"checkout-api","last_deploy_utc":"2026-04-21T17:05Z","flags":[{"id":"retry_backoff_v2","pct":35}]}`},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicIncident},
			"Deploy yesterday 17:05Z; partial rollout of retry_backoff_v2 at 35% — plausible contributor, not definitive.", "", ""},
	}
}

func episodeTicketsAndMetrics() []sessionTurn {
	return []sessionTurn{
		{sourceUser, []string{tagRoleUser, tagTopicProject},
			"What's the status of ticket HEX-442?", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicProject, tagToolMeta},
			"Work item HEX-442 from tracker.",
			"jira.issue", `{"key":"HEX-442","status":"In Review","assignee":"platform","updated":"2026-04-22T10:12Z"}`},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicProject},
			"HEX-442 is In Review on the platform board; last update ~10 minutes ago.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicProject},
			"Pull child blockers linked to HEX-442.", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicProject, tagToolMeta},
			"Dependency graph excerpt.",
			"jira.links", `{"parent":"HEX-442","blockers":["HEX-501","HEX-512"],"types":["blocks","relates"]}`},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicProject},
			"Two linked items: HEX-501 and HEX-512; both tagged as scheduling blockers for the review.", "", ""},
	}
}

func episodeQuotaAndCost() []sessionTurn {
	return []sessionTurn{
		{sourceUser, []string{tagRoleUser, tagTopicQuota},
			"Are we over the monthly LLM token budget for workspace «project-alpha»?", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicQuota, tagToolMeta},
			"Billing rollup for workspace.",
			"billing.usage", `{"workspace":"project-alpha","month":"2026-04","tokens_used":184200000,"cap":200000000,"forecast_overshoot_date":null}`},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicQuota},
			"≈184.2M of 200M tokens used; no overshoot forecast on current burn.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicQuota},
			"Contradiction check: finance said cap was 150M; ops dashboard still shows 200M.", "", ""},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicQuota},
			"I'll surface this as a policy seam: finance vs ops numbers disagree until finance publishes revision.", "", ""},
	}
}

func episodeRetrievalDocs() []sessionTurn {
	return []sessionTurn{
		{sourceRetrieval, []string{tagRoleRetrieval, tagRetrievalKB, tagTopicProject},
			"[doc §3.2] Deployments must pass canary at 5% for 30m before full promotion.", "", ""},
		{sourceRetrieval, []string{tagRoleRetrieval, tagRetrievalKB, tagTopicSecurity},
			"[runbook] Break-glass IAM grants expire automatically after 72h unless renewed.", "", ""},
		{sourceRetrieval, []string{tagRoleRetrieval, tagRetrievalKB, tagTopicIncident},
			"[postmortem 2026-03] Checkout latency spikes correlated with partial feature-flag ramp twice.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicProject},
			"Incorporate the retrieval snippets about canary policy into the stand-up note.", "", ""},
	}
}

func episodeModerationAndMemory() []sessionTurn {
	return []sessionTurn{
		{sourceModeration, []string{tagModeration, tagTopicProject},
			"Policy check: content contains credential-like pattern; rotate if this was real (redacted in store).", "", ""},
		{sourceMemory, []string{tagMemorySession, tagTopicProject},
			"Session durable note: signing key rotation complete; IAM exceptions need weekly review.", "", ""},
		{sourceMemory, []string{tagMemorySession, tagTopicIncident},
			"Durable note: HEX-991 triage anchored on gateway wait in charge span — follow up with retry flag exposure.", "", ""},
	}
}

func episodeStandupAndCalendar() []sessionTurn {
	return []sessionTurn{
		{sourceUser, []string{tagRoleUser, tagTopicProject},
			"Pin this: deployments freeze Thursday 18:00 UTC.", "", ""},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicProject},
			"Pinned. Deployment freeze Thursday 18:00 UTC noted for coordination.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicProject},
			"Contradiction check: earlier we said codename Aurora; product now calls it Nova.", "", ""},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicProject},
			"I'll treat Nova as current codename and supersede Aurora in downstream summaries.", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicProject, tagToolMeta},
			"Calendar lookup for stand-up.",
			"calendar.event", `{"title":"platform stand-up","start_utc":"2026-04-23T09:30:00Z","room":"meet/ws-alpha"}`},
		{sourceUser, []string{tagRoleUser, tagTopicProject},
			"Please summarize open risks in one paragraph for stand-up.", "", ""},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicProject},
			"Risks: API freeze window, HEX-442 review latency, codename rename communication.", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicProject, tagToolMeta},
			"Stand-up logistics.",
			"calendar.link", `{"event":"platform stand-up","attach":"https://workspace.example/wiki/standup-2026-04-23"}`},
	}
}

func episodeStressLongTail() []sessionTurn {
	// Extra volume: flaky tests, migrations, codegen, branching strategy — typical long session noise.
	return []sessionTurn{
		{sourceUser, []string{tagRoleUser, tagTopicObs},
			"Our CI flaky rate for «platform» pipelines last 14d?", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicObs, tagToolMeta},
			"CI flaky summary.",
			"ci.stats", `{"pipelines":["platform"],"window":"14d","flaky_rate_pct":3.8,"worst_suite":"integration-k8s"}`},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicObs},
			"≈3.8% flaky over 14d; worst offender integration-k8s suite.", "", ""},
		{sourceDeveloper, []string{tagRoleDeveloper, tagTopicProject},
			"When editing migrations, bump schema lock version and run ./hack/verify-schema.sh locally.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicProject},
			"We need a zero-downtime rollout for HEX-701 database column split.", "", ""},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicProject},
			"Recommend phased deploy: dual-write column, backfill job, flip read path, retire old column.", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicProject, tagToolMeta},
			"Migration playbook pointer.",
			"wiki.fetch", `{"slug":"runbooks/db-dual-write","revision":118,"anchors":["dual-write-pattern"]}`},
		{sourceRetrieval, []string{tagRoleRetrieval, tagRetrievalKB, tagTopicProject},
			"[design] Branching strategy: trunk-based with short-lived feature flags behind «experiments» namespace.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicSecurity},
			"Scan container images for checkout-api@sha256:… — any critical CVEs?", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicSecurity, tagToolMeta},
			"Image vulnerability summary.",
			"scanner.report", `{"image":"checkout-api","digest":"sha256:…","critical":0,"high":2,"notes":"upgrade base alpine"}`},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicSecurity},
			"No critical; two highs tied to base image — schedule rebuild with patched alpine.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicIncident},
			"Attach incident timeline bullets for HEX-991 comms.", "", ""},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicIncident},
			"Timeline: T+0 alert, T+12m trace triage, T+45m partial flag rollback discussed, T+90m steady state.", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicIncident, tagToolMeta},
			"Pager incidents linked.",
			"pager.related", `{"incident":"HEX-991","linked":["HEX-887"],"similarity":"gateway_latency"}`},
		{sourceMemory, []string{tagMemorySession, tagTopicQuota},
			"Durable note: finance vs ops token cap conflict logged — needs authoritative budget source.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicQuota},
			"If we throttle summarization, which workspaces lose headroom first?", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicQuota, tagToolMeta},
			"Workspace ranking by remaining token headroom.",
			"billing.rank", `{"order":"asc_remaining","top":[{"ws":"sandbox","pct_left":12},{"ws":"research","pct_left":21}]}`},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicQuota},
			"Sandbox and research workspaces have lowest remaining headroom under current pacing.", "", ""},
		{sourceSystem, []string{tagRoleSystem, tagTopicProject},
			"[maintenance banner] Planned DB compaction Sunday 02:00–04:00 UTC — expect elevated read latency.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicObs},
			"Compare error budget burn for checkout vs payments last 90d.", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicObs, tagToolMeta},
			"SLO burn comparison.",
			"slo.report", `{"services":["checkout-api","payments-edge"],"window":"90d","burn_multiple_checkout":0.7,"burn_multiple_payments":1.9}`},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicObs},
			"Payments-edge error budget burns faster than checkout in the trailing 90d window.", "", ""},
		{sourceModeration, []string{tagModeration, tagTopicIncident},
			"Automated withhold: pasted exception stack trace resembles production keys — stripped from persisted cell.", "", ""},
		{sourceRetrieval, []string{tagRoleRetrieval, tagRetrievalKB, tagTopicSecurity},
			"[policy excerpt] Quarterly access reviews mandatory for IAM admin roles.", "", ""},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicProject},
			"Standing summary: prioritize HEX-442 review and Nova rename blast radius before freeze.", "", ""},
		{sourceDeveloper, []string{tagRoleDeveloper, tagTopicObs},
			"Profiling tip: capture pprof heap when integration-k8s flakes exceed threshold in nightly.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicProject},
			"Generate a terse checklist for on-call handoff tonight.", "", ""},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicProject},
			"Checklist: watch gateway p99, HEX-991 follow-up ticket, HEX-442 nudge, freeze comms pinned.", "", ""},
	}
}

func episodeClosureAndRisks() []sessionTurn {
	return []sessionTurn{
		{sourceUser, []string{tagRoleUser, tagTopicObs},
			"Snapshot RED metrics vs last week for checkout.", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicObs, tagToolMeta},
			"WoW comparison checkout service.",
			"metrics.compare", `{"service":"checkout-api","window":"7d","p95_delta_pct":-6.2,"error_delta_pct":0.1}`},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicObs},
			"p95 improved ~6% week-over-week; error rate essentially flat.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicProject},
			"Draft a brief for leadership: incident follow-ups + ticket health.", "", ""},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicProject},
			"Leadership brief skeleton: (1) HEX-991 mitigations, (2) HEX-442 stuck in review, (3) rename Nova comms.", "", ""},
		{sourceUser, []string{tagRoleUser, tagTopicSecurity},
			"Confirm SOC2 evidence pack link still valid.", "", ""},
		{sourceTool, []string{tagRoleTool, tagTopicSecurity, tagToolMeta},
			"Evidence bundle metadata.",
			"compliance.link", `{"framework":"SOC2","bundle_id":"soc2-2026-q1","expires_utc":"2026-06-01T00:00:00Z","status":"valid"}`},
		{sourceAssistant, []string{tagRoleAssistant, tagTopicSecurity},
			"Evidence bundle valid through June 2026 per compliance metadata.", "", ""},
	}
}
