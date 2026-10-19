import test from 'node:test';
import assert from 'node:assert/strict';
import {
	childrenQuery,
	childrenValues,
	findTarget,
	fieldsQuery,
	graphql,
	issueQuery,
	setValueMutation,
} from './record.mjs';
import { ACTUAL_FIELD } from './lib.mjs';

// The live checks run wherever a token exists, including the workflow, and
// target the issue being recorded. Issue 220 is the local fallback.
const subject = Number(process.env.INPUT_ISSUE ?? 220);
const live = Boolean(process.env.GH_TOKEN);

function balanced(text) {
	let depth = 0;
	for (const character of text.replace(/"[^"]*"/g, '""')) {
		if (character === '{') depth += 1;
		if (character === '}') depth -= 1;
		if (depth < 0) return false;
	}
	return depth === 0;
}

test('every record query is balanced and names its fields', () => {
	const queries = [
		issueQuery(subject),
		childrenQuery(subject),
		fieldsQuery('PVT_example'),
		setValueMutation('PVT_example', 'PVTI_example', 'PVTF_example', 1.5),
	];
	for (const query of queries) assert.equal(balanced(query), true, query);
	assert.match(issueQuery(subject), new RegExp(`issue\\(number: ${subject}\\)`));
	assert.match(childrenQuery(subject), /subIssues\(first: 100\)/);
	assert.match(setValueMutation('p', 'i', 'f', 1.5), /number: 1\.5/);
});

test('the issue query resolves the project item of a real issue', { skip: !live }, () => {
	const issue = graphql(issueQuery(subject)).repository.issue;
	assert.equal(issue.number, subject);
	assert.ok(issue.projectItems.nodes.length > 0, `issue #${subject} has no project item`);
	assert.ok(issue.parent.number > 0, `issue #${subject} has no parent`);
});

test('the fields query finds the measured field on the project', { skip: !live }, () => {
	const issue = graphql(issueQuery(subject)).repository.issue;
	const projectId = issue.projectItems.nodes[0].project.id;
	assert.match(findTarget(projectId, ACTUAL_FIELD), /^PVTF_/);
});

test('the children query returns the child items behind a roll-up', { skip: !live }, () => {
	const issue = graphql(issueQuery(subject)).repository.issue;
	const projectId = issue.projectItems.nodes[0].project.id;
	const values = childrenValues(issue.parent.number, projectId);
	assert.ok(Array.isArray(values));
	for (const value of values) assert.equal(typeof value, 'number');
});
