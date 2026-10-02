# GitHub backlog bootstrap

Creates labels, the **[jobifai Roadmap](https://github.com/users/mmbspk/projects/2)** project, epics, story sub-issues, and the index issue ([#57](https://github.com/mmbspk/jobifai/issues/57)).

## Local test (already bootstrapped)

The live backlog already exists on GitHub. To verify locally:

```bash
gh issue view 57 --repo mmbspk/jobifai
gh project item-list 2 --owner @me --limit 5
open "https://github.com/users/mmbspk/projects/2"
```

## Fresh repo / fork

Requires `gh auth login` with `project` scope:

```bash
gh auth refresh -s project
./scripts/github-backlog/bootstrap.sh
```

The script exits without changes if any issue labeled `epic` already exists.
