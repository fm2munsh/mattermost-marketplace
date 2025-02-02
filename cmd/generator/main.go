package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/blang/semver"
	"github.com/google/go-github/v28/github"
	"github.com/h2non/filetype"
	svg "github.com/h2non/go-is-svg"
	mattermostModel "github.com/mattermost/mattermost-server/model"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"github.com/mattermost/mattermost-marketplace/internal/model"
)

func init() {
	generatorCmd.PersistentFlags().String("github-token", "", "The optional GitHub token for API requests.")
	generatorCmd.PersistentFlags().Bool("debug", false, "Whether to output debug logs.")
	generatorCmd.PersistentFlags().Bool("include-pre-release", true, "Whether to include pre-release versions.")
	generatorCmd.PersistentFlags().String("existing", "", "An existing plugins.json to help streamline incremental updates.")
}

func main() {
	if err := generatorCmd.Execute(); err != nil {
		logger.WithError(err).Error("command failed")
		os.Exit(1)
	}
}

var generatorCmd = &cobra.Command{
	Use:   "generator",
	Short: "Generator is a tool to generate the plugins.json database",
	// SilenceErrors allows us to explicitly log the error returned from generatorCmd below.
	SilenceErrors: true,
	RunE: func(command *cobra.Command, args []string) error {
		command.SilenceUsage = true

		debug, _ := command.Flags().GetBool("debug")
		if debug {
			logger.SetLevel(logrus.DebugLevel)
		}

		includePreRelease, _ := command.Flags().GetBool("include-pre-release")
		githubToken, _ := command.Flags().GetString("github-token")

		var client *github.Client

		if githubToken != "" {
			ctx := context.Background()
			ts := oauth2.StaticTokenSource(
				&oauth2.Token{AccessToken: githubToken},
			)
			tc := oauth2.NewClient(ctx, ts)

			client = github.NewClient(tc)
		} else {
			client = github.NewClient(nil)
		}

		var existingPlugins []*model.Plugin
		existingDatabase, _ := command.Flags().GetString("existing")
		if existingDatabase != "" {
			file, err := os.Open(existingDatabase)
			if err != nil {
				return errors.Wrapf(err, "failed to open existing database %s", existingDatabase)
			}
			defer file.Close()

			existingPlugins, err = model.PluginsFromReader(file)
			if err != nil {
				return errors.Wrapf(err, "failed to read existing database %s", existingDatabase)
			}
		}

		ctx := context.Background()

		repositoryNames := []string{
			"mattermost-plugin-github",
			"mattermost-plugin-autolink",
			"mattermost-plugin-zoom",
			"mattermost-plugin-jira",
			"mattermost-plugin-welcomebot",
			"mattermost-plugin-jenkins",
			"mattermost-plugin-antivirus",
			"mattermost-plugin-custom-attributes",
			"mattermost-plugin-aws-SNS",
			"mattermost-plugin-gitlab",
			"mattermost-plugin-nps",
			"mattermost-plugin-webex",
		}

		iconPaths := map[string]string{
			"mattermost-plugin-aws-SNS": "data/icons/aws-sns.svg",
			"mattermost-plugin-github":  "data/icons/github.svg",
			"mattermost-plugin-gitlab":  "data/icons/gitlab.svg",
			"mattermost-plugin-jenkins": "data/icons/jenkins.svg",
			"mattermost-plugin-jira":    "data/icons/jira.svg",
			"mattermost-plugin-webex":   "data/icons/webex.svg",
		}

		plugins := []*model.Plugin{}

		for _, repositoryName := range repositoryNames {
			logger.Debugf("querying repository %s", repositoryName)

			releasePlugins, err := getReleasePlugins(ctx, client, repositoryName, includePreRelease, existingPlugins)
			if err != nil {
				return errors.Wrapf(err, "failed to release plugin for repository %s", repositoryName)
			}

			for _, plugin := range releasePlugins {
				if len(plugin.IconData) == 0 {
					if iconPath, ok := iconPaths[repositoryName]; ok {
						icon, err := getIcon(ctx, iconPath)
						if err != nil {
							return errors.Wrapf(err, "failed to fetch icon for repository %s", repositoryName)
						}
						if svg.Is(icon) {
							plugin.IconData = fmt.Sprintf("data:image/svg+xml;base64,%s", base64.StdEncoding.EncodeToString(icon))
						} else {
							kind, err := filetype.Image(icon)
							if err != nil {
								return errors.Wrapf(err, "failed to match icon at %s to image", iconPath)
							}

							plugin.IconData = fmt.Sprintf("data:%s;base64,%s", kind.MIME, base64.StdEncoding.EncodeToString(icon))
						}
					}
				}
				plugins = append(plugins, plugin)
			}
		}

		encoder := json.NewEncoder(os.Stdout)
		err := encoder.Encode(plugins)
		if err != nil {
			return errors.Wrap(err, "failed to encode plugins result")
		}

		return nil
	},
}

// getReleasePlugins queries GitHub for all releases of the given plugin, sorting by plugin versioning descending.
func getReleasePlugins(ctx context.Context, client *github.Client, repositoryName string, includePreRelease bool, existingPlugins []*model.Plugin) ([]*model.Plugin, error) {
	logger := logger.WithField("repository", repositoryName)

	repository, _, err := client.Repositories.Get(ctx, "mattermost", repositoryName)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get repository")
	}

	releases, err := getReleases(ctx, client, repositoryName, includePreRelease)
	if err != nil {
		return nil, err
	}
	if len(releases) == 0 {
		logger.Warnf("no releases found for repository")
		return nil, nil
	}

	var plugins []*model.Plugin
	// Keep track of the latest plugin compatible with the given server version
	minServerVersionsSeen := map[string]*model.Plugin{}
	for _, release := range releases {
		releasePlugin, err := getReleasePlugin(release, repository, existingPlugins)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get release plugin for %s", release.GetName())
		}

		if releasePlugin == nil {
			logger.Warnf("no plugin found for release %s", release.GetName())
			continue
		}

		if minServerVersionsSeen[releasePlugin.Manifest.MinServerVersion] != nil {
			if releasePlugin.Manifest.Version == "" {
				return nil, errors.Errorf("version is empty for manifest.Id %s", releasePlugin.Manifest.Id)
			}

			lastSeenPlugin := minServerVersionsSeen[releasePlugin.Manifest.MinServerVersion]
			lastSeenPluginVersion, err := semver.Parse(lastSeenPlugin.Manifest.Version)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to parse version %s", lastSeenPlugin.Manifest.Version)
			}

			releasePluginVersion, err := semver.Parse(releasePlugin.Manifest.Version)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to parse release plugin version %s", releasePlugin.Manifest.Version)
			}

			// Ignore if we have the latest plugin version for this server version
			if lastSeenPluginVersion.GTE(releasePluginVersion) {
				continue
			}
		}

		minServerVersionsSeen[releasePlugin.Manifest.MinServerVersion] = releasePlugin
	}

	for _, plugin := range minServerVersionsSeen {
		plugins = append(plugins, plugin)
	}

	// Sort the final slice by plugin version, descending
	sort.SliceStable(
		plugins,
		func(i, j int) bool {
			return semver.MustParse(plugins[i].Manifest.Version).GT(semver.MustParse(plugins[j].Manifest.Version))
		},
	)

	return plugins, nil
}

// getReleases returns all GitHub releases for the given repository.
func getReleases(ctx context.Context, client *github.Client, repoName string, includePreRelease bool) ([]*github.RepositoryRelease, error) {
	var result []*github.RepositoryRelease
	options := &github.ListOptions{
		Page:    0,
		PerPage: 40,
	}
	for {
		releases, resp, err := client.Repositories.ListReleases(ctx, "mattermost", repoName, options)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get releases for repository %s", repoName)
		}

		for _, release := range releases {
			if release.GetDraft() {
				continue
			}

			if release.GetPrerelease() && !includePreRelease {
				continue
			}

			result = append(result, release)
		}

		if resp.NextPage == 0 {
			break
		}
		options.Page = resp.NextPage
	}

	return result, nil
}

func getReleasePlugin(release *github.RepositoryRelease, repository *github.Repository, existingPlugins []*model.Plugin) (*model.Plugin, error) {
	var releaseName string
	if release.GetName() == "" {
		releaseName = release.GetTagName()
	} else {
		releaseName = fmt.Sprintf("%s (%s)", release.GetName(), release.GetTagName())
	}
	logger.Debugf("found latest release %s", releaseName)

	downloadURL := ""
	var signatureAsset *github.ReleaseAsset
	releaseNotesURL := release.GetHTMLURL()
	var updatedAt time.Time
	for _, releaseAsset := range release.Assets {
		assetName := releaseAsset.GetName()
		if strings.Contains(assetName, "-amd64") {
			logger.Debugf("ignoring old style tar bundle %s, for release %s", assetName, releaseName)
			continue
		}

		if strings.HasSuffix(assetName, ".tar.gz") {
			downloadURL = releaseAsset.GetBrowserDownloadURL()
			timestampUpdatedAt := releaseAsset.GetUpdatedAt()
			if timestampUpdatedAt.IsZero() {
				timestampUpdatedAt = releaseAsset.GetCreatedAt()
			}

			updatedAt = timestampUpdatedAt.In(time.UTC)
		}
		if strings.HasSuffix(assetName, ".sig") || strings.HasSuffix(assetName, ".asc") {
			if signatureAsset != nil {
				return nil, errors.Errorf("found multiple signatures %s for release %s", assetName, releaseName)
			}
			signatureAsset = &releaseAsset
		}
	}

	var signature string
	if signatureAsset != nil {
		var err error
		signature, err = downloadSignature(signatureAsset)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to download signatures for release %s", releaseName)
		}
	}

	if downloadURL == "" {
		logger.Warnf("Failed to find plugin asset release %s", releaseName)
		return nil, nil
	}

	var plugin *model.Plugin
	for _, p := range existingPlugins {
		if p.DownloadURL == downloadURL {
			plugin = p
			break
		}
	}

	// If no plugin in existing database or the updated timestamp has changed, attempt to download and inspect manifest.
	if plugin == nil || updatedAt.IsZero() || plugin.UpdatedAt.Before(updatedAt) {
		if plugin == nil {
			logger.Debug("no existing plugin")
		} else if updatedAt.IsZero() {
			logger.Debug("no new update timestamp for plugin")
		} else if plugin.UpdatedAt.IsZero() {
			logger.Debug("no recorded update timestamp for plugin")
		} else if plugin.UpdatedAt.Before(updatedAt) {
			logger.Debugf("plugin release asset is newer (+%d seconds)", updatedAt.Sub(plugin.UpdatedAt)/time.Second)
		}

		logger.Debugf("fetching download url %s", downloadURL)

		plugin = &model.Plugin{}

		resp, err := http.Get(downloadURL)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to download plugin bundle for release %s", releaseName)
		}
		defer resp.Body.Close()

		gzBundleReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read gzipped plugin bundle for release %s", releaseName)
		}

		bundleData, err := ioutil.ReadAll(gzBundleReader)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read plugin bundle for release %s", releaseName)
		}

		manifestData, err := getFromTarFile(tar.NewReader(bytes.NewReader(bundleData)), "plugin.json")
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read manifest from plugin bundle for release %s", releaseName)
		}
		plugin.Manifest = mattermostModel.ManifestFromJson(bytes.NewReader(manifestData))
		if plugin.Manifest == nil {
			return nil, errors.Errorf("manifest nil after reading from plugin bundle for release %s", releaseName)
		}

		if plugin.Manifest.IconPath != "" {
			iconData, err := getFromTarFile(tar.NewReader(bytes.NewReader(bundleData)), plugin.Manifest.IconPath)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to read icon data from plugin bundle for release %s", releaseName)
			}

			logger.Debugf("using icon specified in manifest as %s", plugin.Manifest.IconPath)
			plugin.IconData = fmt.Sprintf("data:image/svg+xml;base64,%s", base64.StdEncoding.EncodeToString(iconData))
		}
	} else {
		logger.Debugf("skipping download since found existing plugin")
	}

	if plugin.Manifest == nil {
		return nil, fmt.Errorf("failed to find plugin manifest for release %s", releaseName)
	}

	// Reset fields, even if we found the existing plugin above.
	if plugin.Manifest.HomepageURL != "" {
		plugin.HomepageURL = plugin.Manifest.HomepageURL
	} else {
		plugin.HomepageURL = repository.GetHTMLURL()
	}
	plugin.DownloadURL = downloadURL
	plugin.ReleaseNotesURL = releaseNotesURL
	plugin.Signature = signature
	plugin.UpdatedAt = updatedAt

	return plugin, nil
}

func getFromTarFile(reader *tar.Reader, filepath string) ([]byte, error) {
	for {
		hdr, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read tar file")
		}

		// Match the filepath, assuming the tar file contains a leading folder matching the
		// plugin id.
		matched, err := path.Match(fmt.Sprintf("*/%s", filepath), hdr.Name)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to match file %s in tar file", filepath)
		} else if !matched {
			continue
		}

		data, err := ioutil.ReadAll(reader)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to read %s in tar file", filepath)
		}
		return data, nil
	}

	return nil, errors.Errorf("failed to find %s in tar file", filepath)
}

func downloadSignature(asset *github.ReleaseAsset) (string, error) {
	signature, err := getSignatureFromAsset(*asset)
	if err != nil {
		return "", errors.Wrap(err, "Can't get signature from the asset")
	}

	return signature, nil
}

func getSignatureFromAsset(asset github.ReleaseAsset) (string, error) {
	url := asset.GetBrowserDownloadURL()
	logger.Debugf("fetching signature file from %s", url)

	resp, err := http.Get(url)
	if err != nil {
		return "", errors.Wrapf(err, "failed to download signature file %s", asset.GetName())
	}
	defer resp.Body.Close()

	sigFile, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrapf(err, "failed to open downloaded signature file %s", asset.GetName())
	}
	return base64.StdEncoding.EncodeToString(sigFile), nil
}

func getLatestRelease(ctx context.Context, client *github.Client, repoName string, includePreRelease bool) (*github.RepositoryRelease, error) {
	releases, _, err := client.Repositories.ListReleases(ctx, "mattermost", repoName, &github.ListOptions{
		Page:    0,
		PerPage: 10,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get releases for repository %s", repoName)
	}

	var latestRelease *github.RepositoryRelease
	for _, release := range releases {
		if release.GetDraft() {
			continue
		}

		if release.GetPrerelease() && !includePreRelease {
			continue
		}

		if latestRelease == nil || release.GetPublishedAt().After(latestRelease.GetPublishedAt().Time) {
			latestRelease = release
		}
	}

	return latestRelease, nil
}

func getIcon(ctx context.Context, icon string) ([]byte, error) {
	if strings.HasPrefix(icon, "http") {
		logger.Debugf("fetching icon from url %s", icon)

		resp, err := http.Get(icon)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to download plugin icon at %s", icon)
		}
		defer resp.Body.Close()

		return ioutil.ReadAll(resp.Body)
	}

	logger.Debugf("fetching icon from path %s", icon)
	data, err := ioutil.ReadFile(icon)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open icon at path %s", icon)
	}

	return data, nil
}
