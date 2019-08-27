# How to contribute

Windows Machine Config Operator is Apache 2.0 licensed and accepts contributions via GitHub pull requests. This document
outlines some of the conventions on commit message formatting, instructions on how to set up a dev environment, and
other resources to help get contributions into the project.  

## Setting up a dev environment

- Fork the repository on GitHub
- Clone the forked repository in your go path.
- Install [Operator-SDK](https://github.com/operator-framework/operator-sdk).

## Reporting bugs and creating issues

If any part of the project has bugs or documentation mistakes, please let us know by opening a
[Jira issue](https://jira.coreos.com/projects/WINC/summary) (Red Hat internal) or a
[GitHub issue](https://github.com/openshift/windows-machine-config-operator/issues/new) (external users) or a PR.

## Contribution flow

This is an outline of what a contributor's workflow looks like:

- Fork the repository on GitHub
- Clone the forked repository in your go path.
- Create a topic branch from where to base the contribution. This is usually master.
- Make commits of logical units. A commit should typically add a feature or fix a bug, but never both at the same
time. Vendor commits should always be separate.
- A PR should consist of a set of logical commits that makes it easy to review and should not follow your personal
development flow.
- When changes are requested, please amend the commit instead of adding new commits with the changes.
- Make sure commit messages are in the proper format (see below).
- Push changes in a topic branch to a personal fork of the repository.
- To make sure that your topic branch is in sync with the remote master branch, follow a rebase workflow.
- Submit a pull request to openshift/windows-machine-config-operator.
- The PR must receive one `/lgtm` and one `/approve` comments from the maintainers of the project.

Thanks for contributing!

### Format of the commit message

We follow a convention for commit messages that is designed to answer two questions: what changed and why. The
subject line should feature the what and the body of the commit should describe the why.

The format can be described more formally as follows:

```
<subsystem>: <what changed>
<BLANK LINE>
<why this change was made>
<BLANK LINE>
<footer>
```

The first line is the subject and should be no longer than 50 characters, the second line is always blank, and other
lines should be wrapped at 80 characters. This allows the message to be easier to read on GitHub as well as in various
git tools.
