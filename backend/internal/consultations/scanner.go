package consultations

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// Scanner retains directory cursors so bounded passes eventually catch up.
type Scanner struct {
	Store       *Store
	DataDir     string
	Host        string
	root        *os.Root
	dates       *os.File
	names       *os.File
	date        string
	schemaReady bool
}

func (s *Scanner) close() {
	if s.names != nil {
		_ = s.names.Close()
		s.names = nil
	}
	if s.dates != nil {
		_ = s.dates.Close()
		s.dates = nil
	}
	if s.root != nil {
		_ = s.root.Close()
		s.root = nil
	}
}

// Run scans immediately, then retries every thirty seconds until cancelled.
func (s *Scanner) Run(ctx context.Context) {
	defer s.close()
	timer := time.NewTicker(30 * time.Second)
	defer timer.Stop()
	for {
		s.Scan(ctx)
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

func (s *Scanner) open() error {
	data, err := os.OpenRoot(s.DataDir)
	if err != nil {
		return err
	}
	defer func() { _ = data.Close() }()
	for _, path := range []string{"architecture-consults", "architecture-consults/archive"} {
		st, err := data.Lstat(path)
		if err != nil {
			return err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return os.ErrInvalid
		}
	}
	s.root, err = data.OpenRoot(filepath.ToSlash("architecture-consults/archive"))
	if err != nil {
		return err
	}
	s.dates, err = s.root.Open(".")
	return err
}

func (s *Scanner) Scan(parent context.Context) {
	started := time.Now()
	status := ScanStatus{Enabled: true, LastScan: started.UTC().Format(time.RFC3339Nano)}
	defer func() { status.DurationMS = time.Since(started).Milliseconds(); s.Store.setStatus(status) }()
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	if !s.schemaReady {
		if err := s.Store.Schema(ctx); err != nil {
			status.Error = "Graph schema unavailable; scan will retry"
			return
		}
		s.schemaReady = true
	}
	if s.root == nil {
		if err := s.open(); err != nil {
			s.close()
			if !os.IsNotExist(err) {
				status.Error = "Archive root unavailable or unsafe"
			}
			return
		}
	}
	for step := 0; step < 32; step++ {
		if ctx.Err() != nil {
			status.Error = "Scan time budget exhausted; catch-up will continue"
			return
		}
		if s.names == nil {
			dates, err := s.dates.ReadDir(1)
			if err != nil {
				s.close()
				return
			}
			date := dates[0]
			if !date.IsDir() || !datePattern.MatchString(date.Name()) {
				continue
			}
			s.date = date.Name()
			s.names, err = s.root.Open(s.date)
			if err != nil {
				status.Error = "Archive date unavailable"
				s.names = nil
				continue
			}
		}
		names, err := s.names.ReadDir(1)
		if err != nil {
			_ = s.names.Close()
			s.names = nil
			continue
		}
		entry := names[0]
		if !entry.IsDir() || !segment.MatchString(entry.Name()) {
			continue
		}
		status.Examined++
		detail, err := ReadArchive(ctx, s.root, s.Host, s.date, entry.Name(), started)
		if err != nil {
			status.Error = "Archive validation failed"
			continue
		}
		if err = s.Store.Import(ctx, detail); err != nil {
			status.Error = "Graph import failed; scan will retry"
			s.close()
			return
		}
		status.Imported++
	}
}
