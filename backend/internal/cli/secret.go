package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
)

type githubSecretRequest struct {
	Token string `json:"token"`
}

type githubSecretMutationResponse struct {
	Status  string `json:"status"`
	Host    string `json:"host"`
	Account string `json:"account"`
}

type githubSecretAccount struct {
	Host  string `json:"host"`
	Login string `json:"login"`
}

type githubSecretAccountsResponse struct {
	Accounts []githubSecretAccount `json:"accounts"`
}

func newSecretCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage machine-local secrets used by projects",
		Long: "Manage credentials stored on this machine under the AO data directory. " +
			"Project configuration carries only an account login; the secret stays here " +
			"and is injected into that project's sessions.",
	}
	cmd.AddCommand(newSecretSetCommand(ctx))
	cmd.AddCommand(newSecretListCommand(ctx))
	cmd.AddCommand(newSecretRemoveCommand(ctx))
	return cmd
}

func newSecretSetCommand(ctx *commandContext) *cobra.Command {
	var githubHost string
	cmd := &cobra.Command{
		Use:   "set github <login>",
		Short: "Store a GitHub token for an account on this machine",
		Long: "Store a GitHub token for <login> on this machine. The token is read from " +
			"stdin, never from an argument, so it does not appear in shell history or " +
			"the process list:\n\n" +
			"  gh auth token | ao secret set github work-account\n" +
			"  gh auth token --hostname ghe.example.com | ao secret set github me --host ghe.example.com",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(2)(cmd, args); err != nil {
				return usageError{err}
			}
			if !strings.EqualFold(strings.TrimSpace(args[0]), "github") {
				return usageError{fmt.Errorf("unsupported secret provider %q (supported: github)", args[0])}
			}
			if strings.TrimSpace(args[1]) == "" {
				return usageError{errors.New("usage: secret login is required")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			login := strings.TrimSpace(args[1])
			host := strings.TrimSpace(githubHost)
			prompt := login
			if host != "" {
				prompt = login + " on " + host
			}
			if file, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(file.Fd()) {
				if _, err := fmt.Fprintf(cmd.ErrOrStderr(), "GitHub token for %s: ", prompt); err != nil {
					return err
				}
			}
			secret, err := ctx.deps.ReadSecret(cmd.InOrStdin())
			if file, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(file.Fd()) {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr())
			}
			if err != nil {
				return fmt.Errorf("read token: %w", err)
			}
			defer zeroBytes(secret)
			token := strings.TrimSpace(string(secret))
			if token == "" {
				return errors.New("token is required on stdin")
			}
			path := "secrets/github/" + url.PathEscape(login)
			if host != "" {
				path += "?host=" + url.QueryEscape(host)
			}
			var res githubSecretMutationResponse
			if err := ctx.putJSON(cmd.Context(), path, githubSecretRequest{Token: token}, &res); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "stored github/%s/%s on this machine\n", res.Host, login)
			return err
		},
	}
	cmd.Flags().StringVar(&githubHost, "host", "", "GitHub host (default github.com)")
	return cmd
}

func newSecretListCommand(ctx *commandContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "List stored secret accounts without values",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var res githubSecretAccountsResponse
			if err := ctx.getJSON(cmd.Context(), "secrets/github", &res); err != nil {
				return err
			}
			if len(res.Accounts) == 0 {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), "No GitHub tokens stored on this machine.")
				return err
			}
			for _, account := range res.Accounts {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "github/%s/%s\n", account.Host, account.Login); err != nil {
					return err
				}
			}
			return nil
		},
	}
	return cmd
}

func newSecretRemoveCommand(ctx *commandContext) *cobra.Command {
	var githubHost string
	cmd := &cobra.Command{
		Use:     "rm github <login>",
		Aliases: []string{"remove", "delete"},
		Short:   "Remove a stored GitHub token from this machine",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.ExactArgs(2)(cmd, args); err != nil {
				return usageError{err}
			}
			if !strings.EqualFold(strings.TrimSpace(args[0]), "github") {
				return usageError{fmt.Errorf("unsupported secret provider %q (supported: github)", args[0])}
			}
			if strings.TrimSpace(args[1]) == "" {
				return usageError{errors.New("usage: secret login is required")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			login := strings.TrimSpace(args[1])
			path := "secrets/github/" + url.PathEscape(login)
			if host := strings.TrimSpace(githubHost); host != "" {
				path += "?host=" + url.QueryEscape(host)
			}
			var res githubSecretMutationResponse
			if err := ctx.deleteJSON(cmd.Context(), path, &res); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "removed github/%s/%s from this machine\n", res.Host, login)
			return err
		},
	}
	cmd.Flags().StringVar(&githubHost, "host", "", "GitHub host (default github.com)")
	return cmd
}
