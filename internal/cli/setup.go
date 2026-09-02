package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/sbeysenov/kino-cli/internal/config"
)

// newSetupCmd is the first run: a short interview, the API keys, and then the
// first fill of the database.
//
// The point of the interview is that the ranking model has a dozen knobs and
// nobody wants to read a TOML file before their first recommendation. Five
// questions cover the choices that actually change what the list looks like;
// everything else keeps its default, and config.toml stays there for whoever
// does want to read it.
func newSetupCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Первый запуск: несколько вопросов о вкусе, ключи и наполнение базы",
		Long: `Настраивает kino с нуля.

Задаёт пять вопросов о том, что вы смотрите, превращает ответы в коэффициенты
ранжирования, спрашивает ключи API и предлагает сразу наполнить базу.

Всё, что здесь выбрано, потом правится руками в config.toml — мастер только
избавляет от необходимости читать его до первой рекомендации.`,
		Example: "  kino setup\n  kino setup --force   # перенастроить существующую установку",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(cmd, force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "перезаписать существующий config.toml")
	return cmd
}

func runSetup(cmd *cobra.Command, force bool) error {
	out := cmd.OutOrStdout()
	path := os.Getenv("KINO_CONFIG")
	if path == "" {
		path = config.DefaultPath()
	}

	exists := false
	if _, err := os.Stat(path); err == nil {
		exists = true
	}
	if exists && !force {
		return fmt.Errorf("%s уже есть — перенастроить: kino setup --force\n"+
			"посмотреть текущую модель: kino config show", path)
	}
	if !interactive() {
		// Silently inventing answers would write a taste profile nobody chose.
		return fmt.Errorf("kino setup спрашивает и ждёт ответов, а ввод не терминал\n" +
			"для скриптов: kino config init — запишет config.toml с дефолтами")
	}

	// Keys already on disk are offered as the default answer, so re-running the
	// wizard to change one question does not cost the user their keys.
	var known config.Secrets
	if cfg, err := config.Load(); err == nil {
		known = config.Secrets{
			TMDBToken:    cfg.TMDBToken,
			OMDBKey:      cfg.OMDBKey,
			KinopoiskKey: cfg.KinopoiskKey,
		}
	}

	a := &asker{in: bufio.NewScanner(cmd.InOrStdin()), out: out}
	answers := askTaste(a)
	secrets := askKeys(a, known)
	if a.err != nil {
		return a.err
	}

	tuning := answers.Tuning()
	if err := tuning.Validate(); err != nil {
		return fmt.Errorf("ответы дали негодную модель: %w", err)
	}
	if err := config.WriteConfig(path, tuning, secrets, force); err != nil {
		return err
	}
	fmt.Fprintf(out, "\nзаписан %s (права 0600)\n", path)
	printProfile(out, tuning)

	if secrets.TMDBToken == "" {
		fmt.Fprintln(out, "\nБез TMDB-ключа наполнить базу нечем.")
		fmt.Fprintln(out, "Впишите его в [secrets] и запустите: kino sync")
		return nil
	}
	if !a.yesno("\nНаполнить базу сейчас? Первый раз это ~10 минут и ~110 МБ", true) {
		fmt.Fprintln(out, "\nКогда будете готовы:  kino update imdb && kino sync")
		return nil
	}
	return firstFill(out, firstFillSteps)
}

// firstFillSteps is what a fresh install needs before it can answer anything:
// the IMDb datasets, then the daily sync.
var firstFillSteps = [][]string{{"update", "imdb"}, {"sync"}}

// firstFill runs the real commands rather than a copy of their logic: a first
// run that diverges from the daily one would be a second thing to keep correct.
func firstFill(out io.Writer, steps [][]string) error {
	for _, args := range steps {
		fmt.Fprintf(out, "\n=== kino %s ===\n", strings.Join(args, " "))
		root := newRootCmd()
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			return fmt.Errorf("kino %s: %w", strings.Join(args, " "), err)
		}
	}
	fmt.Fprintln(out, "\nГотово. Что посмотреть:  kino")
	return nil
}

func askTaste(a *asker) config.Answers {
	var ans config.Answers
	fmt.Fprintln(a.out, "Пять вопросов о том, что вы смотрите.")
	fmt.Fprintln(a.out, "Enter — принять вариант по умолчанию, он помечен *.")

	switch a.choice("1/5 · Что чаще включаете?", 2, []string{
		"громкое, что все обсуждают",
		"поровну — и хиты, и незаметное",
		"редкое и авторское, даже если оценок мало",
	}) {
	case 1:
		ans.Taste = config.TasteWide
	case 2:
		ans.Taste = config.TasteBalanced
	case 3:
		ans.Taste = config.TasteNiche
	}

	switch a.choice("2/5 · Дети смотрят вместе с вами?", 1, []string{
		"нет, список для взрослых",
		"да — семейное и мультфильмы нужны наравне",
		"детского не надо, но взрослую анимацию люблю",
	}) {
	case 1:
		ans.Kids = config.KidsNone
	case 2:
		ans.Kids = config.KidsWith
	case 3:
		ans.Kids = config.KidsAnime
	}

	switch a.choice("3/5 · Русское кино ранжируется по Кинопоиску. Сверять с миром?", 1, []string{
		"да — снижать то, что любят только на Кинопоиске",
		"нет — верить Кинопоиску",
	}) {
	case 1:
		ans.Ru = config.RuCrossCheck
	case 2:
		ans.Ru = config.RuTrust
	}

	switch a.choice("4/5 · Насколько важно, что фильм вышел только что?", 2, []string{
		"важно — интересует то, что вышло на днях",
		"свежесть в плюс, но не решает",
		"неважно — покажите лучшее за месяц",
	}) {
	case 1:
		ans.Fresh = config.FreshHigh
	case 2:
		ans.Fresh = config.FreshNormal
	case 3:
		ans.Fresh = config.FreshAny
	}

	ans.Limit = a.number("5/5 · Сколько фильмов показывать за раз?", 3)
	return ans
}

func askKeys(a *asker, known config.Secrets) config.Secrets {
	fmt.Fprintln(a.out, "\nТеперь ключи. Вводимое видно на экране — рядом никого лишнего?")
	fmt.Fprintln(a.out, "Файл пишется с правами 0600, ключи в него и попадут.")

	s := known
	s.TMDBToken = a.secret("TMDB Read Access Token (v4) — обязателен",
		"themoviedb.org/settings/api", known.TMDBToken)
	s.KinopoiskKey = a.secret("Ключ Кинопоиска — нужен для kino ru, можно пропустить",
		"телеграм-бот @kinopoiskdev_bot, 200 запросов/сутки", known.KinopoiskKey)
	s.OMDBKey = a.secret("Ключ OMDb — необязателен",
		"omdbapi.com/apikey.aspx", known.OMDBKey)
	return s
}

func printProfile(out io.Writer, t config.Tuning) {
	fmt.Fprintf(out, "\nвеса        imdb %.2f · tmdb %.2f · уверенность %.2f · массовость %.2f · свежесть %.2f\n",
		t.Weights.IMDb, t.Weights.TMDB, t.Weights.Confidence, t.Weights.Mainstream, t.Weights.Freshness)
	fmt.Fprintf(out, "допуск      imdb>=%d, tmdb>=%d, кп>=%d голосов\n",
		t.Thresholds.MinIMDbVotes, t.Thresholds.MinTMDBVotes, t.Thresholds.MinKPVotes)
	fmt.Fprintf(out, "окно        %d дней (%d для kino ru), в списке %d фильмов\n",
		t.Defaults.Period, t.Defaults.RuPeriod, t.Defaults.Limit)
	fmt.Fprintf(out, "штрафы      артхаус %.2f · детское %.1f · анимация %.1f · разрыв с миром %.1f\n",
		t.Arthouse.MaxPenalty, t.Kids.Penalty, t.Kids.AnimationPenalty, t.GapPenalty.MaxPenalty)
	fmt.Fprintln(out, "\nвсё это правится в config.toml, проверить: kino config show")
}

// interactive reports whether stdin is a terminal. A wizard reading a pipe
// would answer its own questions with whatever the pipe happened to contain.
//
// The obvious os.Stdin.Stat() check on ModeCharDevice is not enough: /dev/null
// is a character device too, so under "go test" — and under cron — it reports a
// terminal and the interview runs against silence.
func interactive() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
}

// asker is the terminal side of the interview. It records the first read error
// instead of returning one from every question, so the question list stays
// readable; runSetup checks a.err before using the answers.
type asker struct {
	in  *bufio.Scanner
	out io.Writer
	err error
}

func (a *asker) read() string {
	if a.err != nil {
		return ""
	}
	if !a.in.Scan() {
		if err := a.in.Err(); err != nil {
			a.err = err
		} else {
			a.err = fmt.Errorf("ввод закончился до конца опроса")
		}
		return ""
	}
	return strings.TrimSpace(a.in.Text())
}

// choice asks a numbered question and keeps asking until an offered number
// comes back. def is 1-based.
func (a *asker) choice(q string, def int, opts []string) int {
	for a.err == nil {
		fmt.Fprintf(a.out, "\n%s\n", q)
		for i, o := range opts {
			mark := " "
			if i+1 == def {
				mark = "*"
			}
			fmt.Fprintf(a.out, "  %s %d) %s\n", mark, i+1, o)
		}
		fmt.Fprintf(a.out, "> ")

		line := a.read()
		if line == "" {
			return def
		}
		n, err := strconv.Atoi(line)
		if err == nil && n >= 1 && n <= len(opts) {
			return n
		}
		fmt.Fprintf(a.out, "нужно число от 1 до %d\n", len(opts))
	}
	return def
}

func (a *asker) number(q string, def int) int {
	fmt.Fprintf(a.out, "\n%s [%d]\n> ", q, def)
	line := a.read()
	if line == "" {
		return def
	}
	n, err := strconv.Atoi(line)
	if err != nil || n < 1 {
		fmt.Fprintf(a.out, "не число — оставляю %d\n", def)
		return def
	}
	return n
}

func (a *asker) yesno(q string, def bool) bool {
	hint := "Y/n"
	if !def {
		hint = "y/N"
	}
	fmt.Fprintf(a.out, "%s [%s]\n> ", q, hint)
	switch strings.ToLower(a.read()) {
	case "":
		return def
	case "y", "yes", "д", "да":
		return true
	default:
		return false
	}
}

// secret asks for one API key. An existing key is never printed back, only
// acknowledged: the wizard should not put a key on screen that the user has
// already stored.
func (a *asker) secret(q, where, current string) string {
	fmt.Fprintf(a.out, "\n%s\n  где взять: %s\n", q, where)
	if current != "" {
		fmt.Fprintf(a.out, "  ключ уже есть — Enter, чтобы оставить его\n")
	}
	fmt.Fprintf(a.out, "> ")

	v := a.read()
	if v == "" {
		return current
	}
	if strings.ContainsAny(v, " \t") {
		// A pasted key that arrived with a label or a line break attached is a
		// silent failure otherwise: the request just comes back unauthorised.
		fmt.Fprintln(a.out, "  в ключе пробелы — беру только первое слово")
		v = strings.Fields(v)[0]
	}
	return v
}
