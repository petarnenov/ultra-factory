// Idempotent schema for the AO knowledge graph (ADR 0012).
CREATE CONSTRAINT task_id IF NOT EXISTS FOR (t:Task) REQUIRE t.id IS UNIQUE;
CREATE CONSTRAINT lesson_id IF NOT EXISTS FOR (l:Lesson) REQUIRE l.id IS UNIQUE;
CREATE CONSTRAINT project_id IF NOT EXISTS FOR (p:Project) REQUIRE p.id IS UNIQUE;
CREATE CONSTRAINT issue_key IF NOT EXISTS FOR (i:Issue) REQUIRE i.key IS UNIQUE;
CREATE CONSTRAINT topic_name IF NOT EXISTS FOR (t:Topic) REQUIRE t.name IS UNIQUE;
CREATE INDEX task_outcome IF NOT EXISTS FOR (t:Task) ON (t.outcome);
CREATE FULLTEXT INDEX lesson_text IF NOT EXISTS FOR (l:Lesson) ON EACH [l.text];
CREATE FULLTEXT INDEX task_text IF NOT EXISTS FOR (t:Task) ON EACH [t.title, t.summary];
