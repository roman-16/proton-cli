package drive

import (
	"context"
	"fmt"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
	"github.com/roman-16/proton-cli/internal/units"
)

// A file is not bytes; it is a chain of revisions, one of which is what the file
// holds now. Everything here addresses one link of that chain.

// revisionActive is the state of the revision a file is at now. The others are
// drafts nobody committed and versions something has succeeded.
const revisionActive = 1

// Revision is one version of a file's content.
type Revision struct {
	ID         string `json:"id"`
	State      int    `json:"state"`
	Size       int64  `json:"size"`
	CreateTime int64  `json:"create_time"`
	Author     string `json:"author,omitempty"`
}

// Current reports whether this revision is what the file holds now, which is
// what makes it the one version that can be neither restored nor deleted.
func (r Revision) Current() bool { return r.State == revisionActive }

// FileRevision is one version of one file, looked up before anything is done to
// it: which file holds it, and which of its versions this is.
//
// Looking it up first is what lets a command say which version it is about to
// act on rather than only which reference it was handed, and it is the same
// reason an upload plans before it writes: a dry run has to promise what a real
// run would do.
type FileRevision struct {
	Revision
	// File is the name of the file the revision belongs to.
	File string

	res *Resolved
}

func (s *Service) RevisionsList(ctx context.Context, dc *Context, path string) ([]Revision, error) {
	res, err := s.ResolveFile(ctx, dc, path)
	if err != nil {
		return nil, err
	}
	return s.revisions(ctx, res)
}

func (s *Service) revisions(ctx context.Context, res *Resolved) ([]Revision, error) {
	var r struct {
		Revisions []struct {
			ID               string
			State            int
			Size             int64
			CreateTime       int64
			SignatureAddress string
			SignatureEmail   string
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: fmt.Sprintf("/drive/shares/%s/files/%s/revisions", res.dc.ShareID, res.LinkID),
	}, &r); err != nil {
		return nil, err
	}
	out := make([]Revision, 0, len(r.Revisions))
	for _, rev := range r.Revisions {
		author := rev.SignatureEmail
		if author == "" {
			author = rev.SignatureAddress
		}
		out = append(out, Revision{
			ID: rev.ID, State: rev.State, Size: rev.Size,
			CreateTime: rev.CreateTime, Author: author,
		})
	}
	return out, nil
}

// FindRevision resolves a file and the one of its revisions r names.
//
// The chain is read rather than the reference forwarded, so a revision nobody
// can find is reported the way every other unfound reference is, and a short ID
// stands for a long one here as it does everywhere else.
func (s *Service) FindRevision(ctx context.Context, dc *Context, path, r string) (*FileRevision, error) {
	res, err := s.ResolveFile(ctx, dc, path)
	if err != nil {
		return nil, err
	}
	revs, err := s.revisions(ctx, res)
	if err != nil {
		return nil, err
	}
	var matches []Revision
	for _, rev := range revs {
		if ref.Matches(rev.ID, r) {
			matches = append(matches, rev)
		}
	}
	rev, err := ref.Pick("revision", r, matches,
		func(rev Revision) string { return rev.ID },
		func(rev Revision) string {
			return fmt.Sprintf("%s  %s", units.Time(rev.CreateTime), units.Size(rev.Size))
		})
	if err != nil {
		return nil, err
	}
	return &FileRevision{Revision: rev, File: res.Describe(baseOf(path)), res: res}, nil
}

// RevisionRestore makes an earlier version the file's content again. The version
// it succeeds stays in the history, so a restore is itself undoable.
func (s *Service) RevisionRestore(ctx context.Context, fr *FileRevision) error {
	if fr.Current() {
		return errs.Problemf("%s is already at %s.", fr.File, fr.ID)
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST",
		Path: fmt.Sprintf("/drive/shares/%s/files/%s/revisions/%s/restore",
			fr.res.dc.ShareID, fr.res.LinkID, fr.ID),
	}, nil)
}

// RevisionDelete removes one version from a file's history.
//
// The current one is refused: deleting the bytes a file is made of is deleting
// the file, which is a different command asking a different question.
func (s *Service) RevisionDelete(ctx context.Context, fr *FileRevision) error {
	if fr.Current() {
		return errs.Problemf("%s is the current version of %s.", fr.ID, fr.File).
			Hint("Delete the file itself, or restore an earlier version first.")
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE",
		Path: fmt.Sprintf("/drive/shares/%s/files/%s/revisions/%s",
			fr.res.dc.ShareID, fr.res.LinkID, fr.ID),
	}, nil)
}
