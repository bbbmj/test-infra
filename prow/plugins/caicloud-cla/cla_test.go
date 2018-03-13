package caicloudcla

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/test-infra/prow/github"
)

type ghc struct {
	*testing.T
	labels        sets.String
	commit        *github.CommitData
	pr            *github.PullRequest
	isMember      bool
	LabelsAdded   []string
	LabelsRemoved []string
	createCommentErr, addLabelErr, removeLabelErr,
	getIssueLabelsErr, getCommitErr, getPullRequestErr, isMemberErr error
}

func (c *ghc) CreateComment(_, _ string, _ int, _ string) error {
	c.T.Logf("CreateComment")
	return c.createCommentErr
}

func (c *ghc) AddLabel(_, _ string, _ int, label string) error {
	c.T.Log("AddLabel")

	c.LabelsAdded = append(c.LabelsAdded, fmt.Sprintf("%s", label))
	return nil

	return c.addLabelErr
}

func (c *ghc) RemoveLabel(_, _ string, _ int, label string) error {
	c.T.Log("RemoveLabel")
	c.LabelsRemoved = append(c.LabelsRemoved, fmt.Sprintf("%s", label))
	return c.removeLabelErr
}

func (c *ghc) GetIssueLabels(_, _ string, _ int) (ls []github.Label, err error) {
	c.T.Log("GetIssueLabels")
	for label := range c.labels {
		ls = append(ls, github.Label{Name: label})
	}

	err = c.getIssueLabelsErr
	return
}

func (c *ghc) GetPullRequest(_, _ string, _ int) (*github.PullRequest, error) {
	c.T.Log("GetPullRequestChanges")
	return c.pr, c.getPullRequestErr
}

func (c *ghc) GetCommit(_, _, _ string) (*github.CommitData, error) {
	c.T.Log("GetCommit")
	return c.commit, c.getCommitErr
}

func (c *ghc) IsMember(_, _ string) (bool, error) {
	c.T.Log("IsMember")
	return c.isMember, c.isMemberErr
}

func TestHandlePR(t *testing.T) {
	cases := map[string]struct {
		labels        sets.String
		isMember      bool
		action        github.PullRequestEventAction
		commit        *github.CommitData
		pr            *github.PullRequest
		addedLabels   []string
		removedLabels []string
		err           error
		createCommentErr, addLabelErr, removeLabelErr, getIssueLabelsErr, getCommitErr,
		getPullRequestErr, isMemberErr error
	}{
		"no label, not member": {
			labels:   sets.NewString(),
			isMember: false,
			commit: &github.CommitData{
				Author: github.Author{
					Email: "bmj@gmail.com",
				},
				Committer: github.Committer{
					Email: "bmj@gmail.com",
				},
			},
			pr: &github.PullRequest{
				Number: 101,
			},
			action:        github.PullRequestActionOpened,
			addedLabels:   []string{fmt.Sprintf("%s", claYesLabel)},
			removedLabels: nil,
		},
		"no label, is member, cla not ready": {
			labels:   sets.NewString(),
			isMember: true,
			commit: &github.CommitData{
				Author: github.Author{
					Email: "bmj@gmail.com",
				},
				Committer: github.Committer{
					Email: "bmj@gmail.com",
				},
			},
			pr: &github.PullRequest{
				Number: 101,
			},
			action:        github.PullRequestActionOpened,
			addedLabels:   []string{fmt.Sprintf("%s", claNoLabel)},
			removedLabels: nil,
		},
		"no label, is member, cla not ready (right author email)": {
			labels:   sets.NewString(),
			isMember: true,
			commit: &github.CommitData{
				Author: github.Author{
					Email: "bmj@caicloud.io",
				},
				Committer: github.Committer{
					Email: "bmj@gmail.com",
				},
			},
			pr: &github.PullRequest{
				Number: 101,
			},
			action:        github.PullRequestActionOpened,
			addedLabels:   []string{fmt.Sprintf("%s", claNoLabel)},
			removedLabels: nil,
		},
		"no label, is member, cla not ready (right committer email)": {
			labels:   sets.NewString(),
			isMember: true,
			commit: &github.CommitData{
				Author: github.Author{
					Email: "bmj@gmail.com",
				},
				Committer: github.Committer{
					Email: "bmj@caicloud.io",
				},
			},
			pr: &github.PullRequest{
				Number: 101,
			},
			action:        github.PullRequestActionOpened,
			addedLabels:   []string{fmt.Sprintf("%s", claNoLabel)},
			removedLabels: nil,
		},
		"no label, is member, cla ready": {
			labels:   sets.NewString(),
			isMember: true,
			commit: &github.CommitData{
				Author: github.Author{
					Email: "bmj@caicloud.io",
				},
				Committer: github.Committer{
					Email: "bmj@caicloud.io",
				},
			},
			pr: &github.PullRequest{
				Number: 101,
			},
			action:        github.PullRequestActionOpened,
			addedLabels:   []string{fmt.Sprintf("%s", claYesLabel)},
			removedLabels: nil,
		},
		// if not caicloud member, just pass cla
		"have label `caicloud-cla: yes`, not member, cla ready": {
			labels:   sets.NewString(claYesLabel),
			isMember: false,
			commit: &github.CommitData{
				Author: github.Author{
					Email: "bmj@gmail.com",
				},
				Committer: github.Committer{
					Email: "bmj@gmail.com",
				},
			},
			pr: &github.PullRequest{
				Number: 101,
			},
			action:        github.PullRequestActionEdited,
			addedLabels:   nil,
			removedLabels: nil,
		},
		"have label `caicloud-cla: no`, not member, cla ready": {
			labels:   sets.NewString(claNoLabel),
			isMember: false,
			commit: &github.CommitData{
				Author: github.Author{
					Email: "bmj@gmail.com",
				},
				Committer: github.Committer{
					Email: "bmj@gmail.com",
				},
			},
			pr: &github.PullRequest{
				Number: 101,
			},
			action:        github.PullRequestActionEdited,
			addedLabels:   []string{fmt.Sprintf("%s", claYesLabel)},
			removedLabels: []string{fmt.Sprintf("%s", claNoLabel)},
		},
		"have label `caicloud-cla: no`, is member, cla ready": {
			labels:   sets.NewString(claNoLabel),
			isMember: true,
			commit: &github.CommitData{
				Author: github.Author{
					Email: "bmj@caicloud.io",
				},
				Committer: github.Committer{
					Email: "bmj@caicloud.io",
				},
			},
			pr: &github.PullRequest{
				Number: 101,
			},
			action:        github.PullRequestActionEdited,
			addedLabels:   []string{fmt.Sprintf("%s", claYesLabel)},
			removedLabels: []string{fmt.Sprintf("%s", claNoLabel)},
		},
		"have label `caicloud-cla: yes`, is member, cla not ready": {
			labels:   sets.NewString(claYesLabel),
			isMember: true,
			commit: &github.CommitData{
				Author: github.Author{
					Email: "bmj@gmail.com",
				},
				Committer: github.Committer{
					Email: "bmj@gmail.com",
				},
			},
			pr: &github.PullRequest{
				Number: 101,
			},
			action:        github.PullRequestActionEdited,
			addedLabels:   []string{fmt.Sprintf("%s", claNoLabel)},
			removedLabels: []string{fmt.Sprintf("%s", claYesLabel)},
		},
		"have label `caicloud-cla: yes`, is member, generate commit with GitHub, cla ready": {
			labels:   sets.NewString(claYesLabel),
			isMember: true,
			commit: &github.CommitData{
				Author: github.Author{
					Email: "bmj@caicloud.io",
				},
				Committer: github.Committer{
					Name:  githubName,
					Email: "bmj@caicloud.io",
				},
			},
			pr: &github.PullRequest{
				Number: 101,
			},
			action:        github.PullRequestActionEdited,
			addedLabels:   nil,
			removedLabels: nil,
		},
		"have label `caicloud-cla: no`, is member, generate commit with GitHub, cla ready": {
			labels:   sets.NewString(claNoLabel),
			isMember: true,
			commit: &github.CommitData{
				Author: github.Author{
					Email: "bmj@caicloud.io",
				},
				Committer: github.Committer{
					Name:  githubName,
					Email: "bmj@caicloud.io",
				},
			},
			pr: &github.PullRequest{
				Number: 101,
			},
			action:        github.PullRequestActionEdited,
			addedLabels:   []string{fmt.Sprintf("%s", claYesLabel)},
			removedLabels: []string{fmt.Sprintf("%s", claNoLabel)},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			client := &ghc{
				labels:            c.labels,
				isMember:          c.isMember,
				commit:            c.commit,
				pr:                c.pr,
				createCommentErr:  c.createCommentErr,
				addLabelErr:       c.addLabelErr,
				removeLabelErr:    c.removeLabelErr,
				getIssueLabelsErr: c.getIssueLabelsErr,
				getCommitErr:      c.getCommitErr,
				getPullRequestErr: c.getPullRequestErr,
				isMemberErr:       c.isMemberErr,
				T:                 t,
			}

			event := github.PullRequestEvent{
				Action: c.action,
			}

			if err := handlePR(client, logrus.WithField("plugin", pluginName), event); err != nil {
				t.Errorf("For case %s, didn't expect error from cla plugin: %v", name, err)
			}

			if !reflect.DeepEqual(client.LabelsAdded, c.addedLabels) {
				t.Errorf("Expected: %#v, Got %#v in case %s.", c.addedLabels, client.LabelsAdded, name)
			}

			if !reflect.DeepEqual(client.LabelsRemoved, c.removedLabels) {
				t.Errorf("Expected: %#v, Got %#v in case %s.", c.removedLabels, client.LabelsRemoved, name)
			}
		})
	}
}
