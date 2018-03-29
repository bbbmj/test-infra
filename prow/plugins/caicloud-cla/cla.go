package caicloudcla

import (
	"fmt"
	"regexp"

	"github.com/sirupsen/logrus"

	"k8s.io/test-infra/prow/github"
	"k8s.io/test-infra/prow/pluginhelp"
	"k8s.io/test-infra/prow/plugins"
)

const (
	githubName                 = "GitHub"
	botName                    = "caicloud-bot"
	pluginName                 = "caicloud-cla"
	claYesLabel                = "caicloud-cla: yes"
	claNoLabel                 = "caicloud-cla: no"
	caicloudclaNotFoundMessage = `Thanks for your pull request. Before we can look at your pull request, you'll need to finish a Contributor License Agreement (CLA).

:memo: **Please follow instructions at <https://github.com/caicloud/engineering/blob/master/docs/CLA.md> to complete the CLA.**

<details>

%s
</details>
	`
	maxRetries = 5
)

var emailRe = regexp.MustCompile(`.*@caicloud\.io$`)

func init() {
	plugins.RegisterPullRequestHandler(pluginName, handlePullRequest, helpProvider)
}

func helpProvider(config *plugins.Configuration, enabledRepos []string) (*pluginhelp.PluginHelp, error) {
	// The {WhoCanUse, Usage, Examples, Config} fields are omitted because this plugin cannot be
	// manually triggered and is not configurable.
	return &pluginhelp.PluginHelp{
			Description: "The cla plugin manages the application and removal of the 'caicloud-cla' prefixed labels on pull requests as a reaction to the PR event. It is also responsible for warning unauthorized PR authors that they need to follow the Caicloud CLA before their PR will be merged.",
		},
		nil
}

type githubClient interface {
	CreateComment(owner, repo string, number int, comment string) error
	AddLabel(owner, repo string, number int, label string) error
	RemoveLabel(owner, repo string, number int, label string) error
	GetIssueLabels(org, repo string, number int) ([]github.Label, error)
	GetPullRequest(owner, repo string, number int) (*github.PullRequest, error)
	GetCommit(owner, repo, sha string) (*github.CommitData, error)
	IsMember(org, user string) (bool, error)
}

func handlePullRequest(pc plugins.PluginClient, pe github.PullRequestEvent) error {
	return handlePR(pc.GitHubClient, pc.Logger, pe)
}

// 1. Check that the PR event received from the webhook.
// 2. Use the github search API to get the PR which match the commit hash corresponding to the PR event.
// 3. For PR that matches, check that the PR's HEAD commit hash against the commit hash.
// 4. Set the corresponding CLA label if needed.
func handlePR(gc githubClient, log *logrus.Entry, pe github.PullRequestEvent) error {
	if !isPRChanged(pe) {
		return nil
	}

	var (
		org  = pe.PullRequest.Base.Repo.Owner.Login
		repo = pe.PullRequest.Base.Repo.Name
		num  = pe.PullRequest.Number
	)

	log.Info("PR labels may be out of date. Getting pull request info.")
	pr, err := gc.GetPullRequest(org, repo, num)
	if err != nil {
		log.WithError(err).Warningf("Unable to fetch PR-%d from %s/%s.", num, org, repo)
		return nil
	}

	number := pr.Number

	commit, err := gc.GetCommit(org, repo, pr.Head.SHA)
	if err != nil {
		log.WithError(err).Warningf("Unable to get commit-%s from %s/%s.", pr.Head.SHA, org, repo)
		return nil
	}

	// The CLA enforce the github author name must be caicloud.io domain
	// The committer name can be `GitHub` if the author generate a commit through GitHub
	// The committer can be `caicloud-bot` if cherry-pick.
	claReady := false
	if emailRe.MatchString(commit.Author.Email) {
		if commit.Committer.Email == commit.Author.Email || commit.Committer.Name == githubName || commit.Committer.Name == botName {
			claReady = true
		}
	}

	isMember, err := gc.IsMember(org, pr.User.Login)
	if err != nil {
		log.WithError(err).Errorf("Error from IsMember(%q of org %q).", pr.User.Login, org)
	}
	if !isMember {
		claReady = true
	}

	labels, err := gc.GetIssueLabels(org, repo, num)
	if err != nil {
		log.Warnf("while retrieving labels, error: %v", err)
	}

	hasCaicloudYes := false
	hasCaicloudNo := false
	for _, label := range labels {
		if label.Name == claYesLabel {
			hasCaicloudYes = true
			continue
		}
		if label.Name == claNoLabel {
			hasCaicloudNo = true
			continue
		}

	}

	if hasCaicloudYes && claReady {
		// Nothing to update.
		log.Infof("PR has up-to-date %s label.", claYesLabel)
		return nil
	}

	if hasCaicloudNo && !claReady {
		// Nothing to update.
		log.Infof("PR has up-to-date %s label.", claNoLabel)
		return nil
	}

	if hasCaicloudNo && claReady {
		if err := gc.RemoveLabel(org, repo, number, claNoLabel); err != nil {
			log.WithError(err).Warningf("Could not remove %s label.", claNoLabel)
		}
		if err := gc.AddLabel(org, repo, number, claYesLabel); err != nil {
			log.WithError(err).Warningf("Could not add %s label.", claYesLabel)
		}
		return nil
	}

	if claReady {
		if err := gc.AddLabel(org, repo, number, claYesLabel); err != nil {
			log.WithError(err).Warningf("Could not add %s label.", claYesLabel)
		}
		return nil
	}

	if hasCaicloudYes {
		if err := gc.RemoveLabel(org, repo, number, claYesLabel); err != nil {
			log.WithError(err).Warningf("Could not remove %s label.", claYesLabel)
		}
	}

	if err := gc.CreateComment(org, repo, number, fmt.Sprintf(caicloudclaNotFoundMessage, plugins.AboutThisBot)); err != nil {
		log.WithError(err).Warning("Could not create CLA not found comment.")
	}
	if err := gc.AddLabel(org, repo, number, claNoLabel); err != nil {
		log.WithError(err).Warningf("Could not add %s label.", claNoLabel)
	}

	return nil
}

// These are the only actions indicating the code diffs may have changed.
func isPRChanged(pe github.PullRequestEvent) bool {
	switch pe.Action {
	case github.PullRequestActionOpened:
		return true
	case github.PullRequestActionReopened:
		return true
	case github.PullRequestActionSynchronize:
		return true
	case github.PullRequestActionEdited:
		return true
	default:
		return false
	}
}
