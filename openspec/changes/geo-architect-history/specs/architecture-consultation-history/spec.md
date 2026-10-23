# Spec Delta

## Purpose

Provide a trustworthy, browsable history of `geo.architect` consultations, including their requester, evidence from each consul, objections, rounds, and delivered decision.

## ADDED Requirements

### Requirement: Ingest consultation archives without changing their evidence

The system SHALL ingest allowed original archive artifacts into the configured Neo4j knowledge graph, retain their contents and hashes, and preserve the source archive files. Repeating ingestion of an unchanged archive SHALL not create duplicate consultations, rounds, or artifacts.

#### Scenario: Repeated import
- **WHEN** the same verified archive is scanned twice
- **THEN** the graph exposes one consultation and one copy of each allowed artifact with its original content and hash

#### Scenario: Graph unavailable
- **WHEN** Neo4j is unavailable during a scan
- **THEN** the source archive remains intact and the scan is retryable without losing previously indexed records

### Requirement: Show the recorded requester and process

The system SHALL show the requester, question, creation time, archive host, independent analyses, mutual critiques, proposal versions, ballots, escalation evidence, and final answer when recorded. It SHALL distinguish missing or unparseable data from negative answers or approvals.

#### Scenario: Completed consultation
- **WHEN** a verified consultation has all required artifacts
- **THEN** its detail view shows each consul's analysis and critique, every proposal and ballot round, any escalation, and the verbatim final answer

#### Scenario: Partial consultation
- **WHEN** a consultation lacks a consul report, ballot, requester field, or final answer
- **THEN** the page identifies the specific missing evidence and does not manufacture a vote, consensus, requester, or decision

### Requirement: Count and explain rounds

The system SHALL show the number of observed ratification rounds and the events and consul outputs of every observed phase, in chronological process order.

#### Scenario: Second proposal
- **WHEN** `proposal-2.md` and second-round ballots exist
- **THEN** the page presents both proposal versions, the ballot and objections for each version, and identifies the applicable final outcome

### Requirement: Serve bounded read-only consultation views

The daemon SHALL expose paginated list and detail reads through its established API security boundary and generated client. The UI SHALL render archive Markdown as untrusted content and SHALL not expose local archive paths or process logs.

#### Scenario: Missing graph configuration
- **WHEN** the consultation page requests data and Neo4j is not configured
- **THEN** it shows a clear unavailable state based on the daemon's existing knowledge API error response

#### Scenario: No indexed consultations
- **WHEN** Neo4j is configured but contains no consultation records
- **THEN** the page shows an empty state that explains how an archive appears after a host scan
