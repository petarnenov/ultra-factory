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

#### Scenario: Unsafe or unreadable artifact
- **WHEN** an artifact is unsafe or unreadable after a successful import
- **THEN** its last indexed copy remains available with an unavailable marker, outside content is never read, and no false absence timestamp is created

#### Scenario: Same consultation ID on two hosts
- **WHEN** two AO hosts ingest archives with the same UTC date and consultation ID
- **THEN** the graph keeps separate records keyed by each ingesting daemon host ID

#### Scenario: Large artifact
- **WHEN** an allowed artifact exceeds the content cap
- **THEN** the graph stores a bounded prefix, the hash of the full file, and a truncation flag

#### Scenario: Canonical archive root
- **WHEN** the daemon data directory is `~/.ao/data`
- **THEN** scanning reads the existing `~/.ao/architecture-consults/archive` without relocation or mutation

### Requirement: Show the recorded requester and process

The system SHALL show the requester, question, creation time, archive host, independent analyses, mutual critiques, proposal versions, ballots, escalation evidence, and final answer when recorded. The skill archive contract SHALL require an explicit allowlisted requester string in `state.json` for every new consultation; historical archives without it remain unknown. It SHALL distinguish missing or unparseable data from negative answers or approvals.

#### Scenario: Completed consultation
- **WHEN** a verified consultation has all required artifacts
- **THEN** its detail view shows each consul's analysis and critique, every proposal and ballot round, any escalation, and the verbatim final answer

#### Scenario: Partial consultation
- **WHEN** a consultation lacks a consul report, ballot, requester field, or final answer
- **THEN** the page identifies the specific missing evidence and does not manufacture a vote, consensus, requester, or decision

#### Scenario: Ambiguous ballot
- **WHEN** a ballot does not contain an unambiguous explicit stance for a decision ID
- **THEN** the page shows its original content and marks that stance unknown

#### Scenario: Requester is not structured
- **WHEN** requester appears only in owner or request prose, or the requester field is not a string
- **THEN** requester remains unknown and original request evidence remains readable

#### Scenario: Future consultation records its requester
- **WHEN** a new consultation is created under the skill archive contract
- **THEN** `state.json` includes an explicit allowlisted requester string and the parser and detail page display that recorded value

### Requirement: Count and explain rounds

The system SHALL show the number of observed ratification rounds and the events and consul outputs of every observed phase, in chronological process order.

#### Scenario: Second proposal
- **WHEN** `proposal-2.md` and second-round ballots exist
- **THEN** the page presents both proposal versions, the ballot and objections for each version, and identifies the applicable final outcome

#### Scenario: Fallback arbitration with no council ballots
- **WHEN** an archive has a proposal and an observed report at one of `fallback-arbitration/{fable,astra,deepseek}.md` but no ballot round directories
- **THEN** the original arbiter report is shown as fallback arbitration, the proposal count is one and the ratification round count is zero

### Requirement: Serve bounded read-only consultation views

The daemon SHALL expose paginated list and detail reads through its established API security boundary and generated client. The UI SHALL render archive Markdown as untrusted content and SHALL not expose local archive paths or process logs.

#### Scenario: Missing graph configuration
- **WHEN** the consultation page requests data and Neo4j is not configured
- **THEN** it shows a clear unavailable state based on the daemon's existing knowledge API error response

#### Scenario: Graph unreachable
- **WHEN** Neo4j is configured but a list or detail read cannot reach it
- **THEN** the daemon returns a distinct 503 error and the page shows a retryable unavailable state

#### Scenario: Missing consultation
- **WHEN** a detail request uses a valid ID with no graph record
- **THEN** the daemon returns 404

#### Scenario: No indexed consultations
- **WHEN** Neo4j is configured but contains no consultation records
- **THEN** the page shows an empty state that explains how an archive appears after a host scan

#### Scenario: Complete consultation excludes unused placeholders
- **WHEN** a complete one-round consultation is imported and read through the index
- **THEN** unused round-two and fallback placeholders do not mark it partial

#### Scenario: Unavailable round evidence is retained
- **WHEN** an indexed proposal or ballot round becomes unsafe or unreadable
- **THEN** its unavailable marker survives import, prior content remains retained, and no absence timestamp or new vote is manufactured
