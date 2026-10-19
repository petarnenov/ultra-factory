#!/usr/bin/env node
// Records measured AI tool time on the project board for one issue.

import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { ACTUAL_FIELD, formatHours, round2, sumBlocks } from './lib.mjs';

const MAX_ANCESTORS = 3;
const SELF = process.env.GITHUB_REPOSITORY ?? 'petarnenov/ultra-factory';

function input() {
	return {
		issue: Number(process.env.INPUT_ISSUE),
		blocks: Number(process.env.INPUT_BLOCKS),
		evidence: process.env.INPUT_EVIDENCE ?? '',
	};
}

export function owner() {
	return SELF.split('/')[0];
}

export function name() {
	return SELF.split('/')[1];
}

export function projectOwner() {
	return process.env.PROJECT_OWNER ?? owner();
}

export function projectNumber() {
	return Number(process.env.PROJECT_NUMBER ?? 2);
}

export function issueQuery(number) {
	return `query {
		repository(owner: "${owner()}", name: "${name()}") {
			issue(number: ${number}) {
				number
				parent { number }
				projectItems(first: 20) { nodes { id project { id number } } }
			}
		}
	}`;
}

export function childrenQuery(number) {
	return `query {
		repository(owner: "${owner()}", name: "${name()}") {
			issue(number: ${number}) {
				subIssues(first: 100) {
					nodes {
						number
						projectItems(first: 20) {
							nodes {
								project { id }
								fieldValues(first: 40) {
									nodes {
										... on ProjectV2ItemFieldNumberValue {
											number
											field { ... on ProjectV2FieldCommon { name } }
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}`;
}

export function fieldsQuery(projectId) {
	return `query {
		node(id: "${projectId}") {
			... on ProjectV2 {
				fields(first: 60) { nodes { ... on ProjectV2FieldCommon { id name } } }
			}
		}
	}`;
}

export function setValueMutation(projectId, itemId, fieldId, value) {
	return `mutation {
		updateProjectV2ItemFieldValue(input: {
			projectId: "${projectId}"
			itemId: "${itemId}"
			fieldId: "${fieldId}"
			value: { number: ${value} }
		}) { projectV2Item { id } }
	}`;
}

export function graphql(document) {
	const output = execFileSync('gh', ['api', 'graphql', '-f', `query=${document}`], {
		encoding: 'utf8',
		maxBuffer: 32 * 1024 * 1024,
	});
	const parsed = JSON.parse(output);
	if (parsed.errors) throw new Error(parsed.errors.map((error) => error.message).join('; '));
	return parsed.data;
}

export function requireInputs(values) {
	if (!process.env.GH_TOKEN) {
		throw new Error(
			'GH_TOKEN is empty: add a classic PAT with the project scope as the PROJECT_TOKEN secret',
		);
	}
	if (!Number.isInteger(values.issue)) {
		throw new Error(`INPUT_ISSUE is not an issue number: ${process.env.INPUT_ISSUE}`);
	}
	if (!Number.isFinite(values.blocks) || values.blocks < 0) {
		throw new Error(`INPUT_BLOCKS is not a time in blocks: ${process.env.INPUT_BLOCKS}`);
	}
	if (!/^[A-Za-z0-9._-]+$/.test(projectOwner())) {
		throw new Error(`PROJECT_OWNER is not a login: ${projectOwner()}`);
	}
	if (!Number.isInteger(projectNumber())) {
		throw new Error(`PROJECT_NUMBER is not a number: ${projectNumber()}`);
	}
}

export function projectItem(issue, projectId) {
	const item = (issue.projectItems?.nodes ?? []).find((node) => node.project.id === projectId);
	if (!item) throw new Error(`issue #${issue.number} is not on project ${projectId}`);
	return item;
}

export function childrenValues(number, projectId) {
	const issue = graphql(childrenQuery(number)).repository.issue;
	const values = [];
	for (const child of issue.subIssues?.nodes ?? []) {
		const item = (child.projectItems?.nodes ?? []).find((node) => node.project.id === projectId);
		const field = (item?.fieldValues?.nodes ?? []).find((node) => node.field?.name === ACTUAL_FIELD);
		if (field) values.push(field.number);
	}
	return values;
}

export function findTarget(projectId, fieldName) {
	const project = graphql(fieldsQuery(projectId)).node;
	const field = (project?.fields?.nodes ?? []).find((node) => node.name === fieldName);
	if (!field) throw new Error(`project ${projectId} has no "${fieldName}" field`);
	return field.id;
}

export function setValue(projectId, itemId, fieldId, value) {
	graphql(setValueMutation(projectId, itemId, fieldId, value));
}

export function comment(number, body) {
	const slug = `repos/${owner()}/${name()}/issues/${number}/comments`;
	// The project token needs only project scope, so the comment goes out
	// with the workflow token when one is provided.
	const token = process.env.ISSUES_TOKEN || process.env.GH_TOKEN;
	execFileSync('gh', ['api', '--method', 'POST', slug, '-f', `body=${body}`], {
		encoding: 'utf8',
		env: { ...process.env, GH_TOKEN: token },
	});
}

export function main() {
	const values = input();
	requireInputs(values);
	const target = graphql(issueQuery(values.issue)).repository.issue;
	const first = (target.projectItems?.nodes ?? [])[0];
	if (!first) {
		throw new Error(`issue #${target.number} is on no project; add it to project ${projectNumber()} first`);
	}
	if (first.project.number !== projectNumber()) {
		console.log(`note: issue is on project #${first.project.number}, not #${projectNumber()}`);
	}
	const projectId = first.project.id;
	const itemId = projectItem(target, projectId).id;
	const fieldId = findTarget(projectId, ACTUAL_FIELD);

	setValue(projectId, itemId, fieldId, values.blocks);
	console.log(`#${values.issue}: ${ACTUAL_FIELD} = ${values.blocks}`);

	let parent = target.parent?.number;
	for (let depth = 0; parent && depth < MAX_ANCESTORS; depth += 1) {
		const ancestor = graphql(issueQuery(parent)).repository.issue;
		const ancestorId = projectItem(ancestor, projectId).id;
		const values2 = childrenValues(parent, projectId);
		if (values2.length === 0) break;
		const total = sumBlocks(values2);
		setValue(projectId, ancestorId, fieldId, total);
		console.log(`#${parent}: rolled up ${total} from ${values2.length} child item(s)`);
		parent = ancestor.parent?.number;
	}

	const hours = formatHours(values.blocks * 4 * 3600);
	const header = `**Actual (ai4h): ${round2(values.blocks)} blocks** (${hours}h) recorded for this issue.`;
	comment(values.issue, values.evidence ? `${header}\n\n${values.evidence}` : header);
	console.log(`commented on #${values.issue}`);
}

const isMain = process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href;
if (isMain) {
	try {
		main();
	} catch (error) {
		console.error(error.message);
		process.exitCode = 1;
	}
}
