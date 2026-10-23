# Human escalation and 15-minute takeover

Invoke this council before escalating a complex technical issue to a human. If the issue has already been escalated, record the actual message ID/return channel and UTC send timestamp in `state.json`; set deadline to send time plus 900 seconds, not consultation start time. Persist the last checked human-message cursor and any response timestamp.

Until the deadline, the caller checks the real conversation/channel and any live process at intervals no longer than 60 seconds. A reaction unrelated to the case is not a decision; a human acknowledgement or request for time counts as taking the matter up, and cancels automatic takeover. Follow any human instruction to wait. Do not use an old state file as evidence of a live monitor.

At or after the deadline, re-read the human channel immediately before starting takeover. If no relevant human response exists, mark `timed_out` and automatically convene the three consuls to resolve the technical decision, using the same deliberation protocol. Reuse a completed council only when the project, SHA, patch digest, case, constraints and acceptance criteria still match. Otherwise invalidate affected conclusions and obtain new review and ballots for the changed context. Use a single owner/claim for each escalation so two callers cannot trigger two councils. Record that claim before launching.

Check for a late human response between rounds and immediately before returning the decision or acting on it. Incorporate new constraints, supersede stale proposals and stop conflicting dependent work. Time passing does not grant permission for an action explicitly awaiting human approval; solve the technical choice and report any remaining permission separately.

If the caller cannot read the human response channel, do not infer silence. Return that monitoring limitation and keep the deadline pending until an authorized monitor can check it.

The caller owns monitoring. If it must yield, hand monitoring to a supported persistent job/session and record its live handle. Without that mechanism, keep the caller active and poll at most every 60 seconds; a saved deadline alone does not arm a timer.
