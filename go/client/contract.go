package client

// The generated contract types this package's API reaches, re-exported so an
// application names them through this package and never imports the generated
// one. scripts/idiom_check.py refuses a reachable type this file leaves out.

import (
	api "github.com/openabstractions/abstraction-storage/go/abstraction/storage/content"
)

type AbortOutcome = api.AbortOutcome

const (
	AbortOutcomeAborted   = api.AbortOutcomeAborted
	AbortOutcomeGap       = api.AbortOutcomeGap
	AbortOutcomeForbidden = api.AbortOutcomeForbidden
)

// AbortOutcomeValues returns every member of AbortOutcome in declaration order, in a new slice.
func AbortOutcomeValues() []AbortOutcome { return api.AbortOutcomeValues() }

type AbortResult = api.AbortResult

type Addressing = api.Addressing

const (
	AddressingContent = api.AddressingContent
	AddressingName    = api.AddressingName
)

// AddressingValues returns every member of Addressing in declaration order, in a new slice.
func AddressingValues() []Addressing { return api.AddressingValues() }

type AppendOutcome = api.AppendOutcome

const (
	AppendOutcomeAccepted    = api.AppendOutcomeAccepted
	AppendOutcomeGap         = api.AppendOutcomeGap
	AppendOutcomeForbidden   = api.AppendOutcomeForbidden
	AppendOutcomeInvalid     = api.AppendOutcomeInvalid
	AppendOutcomeOutOfOrder  = api.AppendOutcomeOutOfOrder
	AppendOutcomeTooLarge    = api.AppendOutcomeTooLarge
	AppendOutcomeUnavailable = api.AppendOutcomeUnavailable
)

// AppendOutcomeValues returns every member of AppendOutcome in declaration order, in a new slice.
func AppendOutcomeValues() []AppendOutcome { return api.AppendOutcomeValues() }

type AppendResult = api.AppendResult

type Attestation = api.Attestation

const (
	AttestationDeclared = api.AttestationDeclared
	AttestationObserved = api.AttestationObserved
	AttestationVerified = api.AttestationVerified
)

// AttestationValues returns every member of Attestation in declaration order, in a new slice.
func AttestationValues() []Attestation { return api.AttestationValues() }

type Basis = api.Basis

type BeginOutcome = api.BeginOutcome

const (
	BeginOutcomeStarted     = api.BeginOutcomeStarted
	BeginOutcomeCommitted   = api.BeginOutcomeCommitted
	BeginOutcomePresent     = api.BeginOutcomePresent
	BeginOutcomeForbidden   = api.BeginOutcomeForbidden
	BeginOutcomeInvalid     = api.BeginOutcomeInvalid
	BeginOutcomeConflict    = api.BeginOutcomeConflict
	BeginOutcomeTooLarge    = api.BeginOutcomeTooLarge
	BeginOutcomeBusy        = api.BeginOutcomeBusy
	BeginOutcomeUnsupported = api.BeginOutcomeUnsupported
	BeginOutcomeUnavailable = api.BeginOutcomeUnavailable
	BeginOutcomeExhausted   = api.BeginOutcomeExhausted
)

// BeginOutcomeValues returns every member of BeginOutcome in declaration order, in a new slice.
func BeginOutcomeValues() []BeginOutcome { return api.BeginOutcomeValues() }

type BeginResult = api.BeginResult

type ChangeKind = api.ChangeKind

const (
	ChangeKindAdded   = api.ChangeKindAdded
	ChangeKindRemoved = api.ChangeKindRemoved
)

// ChangeKindValues returns every member of ChangeKind in declaration order, in a new slice.
func ChangeKindValues() []ChangeKind { return api.ChangeKindValues() }

type ChangePageOutcome = api.ChangePageOutcome

const (
	ChangePageOutcomePage        = api.ChangePageOutcomePage
	ChangePageOutcomeGap         = api.ChangePageOutcomeGap
	ChangePageOutcomeForbidden   = api.ChangePageOutcomeForbidden
	ChangePageOutcomeInvalid     = api.ChangePageOutcomeInvalid
	ChangePageOutcomeUnavailable = api.ChangePageOutcomeUnavailable
)

// ChangePageOutcomeValues returns every member of ChangePageOutcome in declaration order, in a new slice.
func ChangePageOutcomeValues() []ChangePageOutcome { return api.ChangePageOutcomeValues() }

type Chunk = api.Chunk

type CloseOutcome = api.CloseOutcome

const (
	CloseOutcomeClosed    = api.CloseOutcomeClosed
	CloseOutcomeGap       = api.CloseOutcomeGap
	CloseOutcomeForbidden = api.CloseOutcomeForbidden
)

// CloseOutcomeValues returns every member of CloseOutcome in declaration order, in a new slice.
func CloseOutcomeValues() []CloseOutcome { return api.CloseOutcomeValues() }

type CloseResult = api.CloseResult

type CommitOutcome = api.CommitOutcome

const (
	CommitOutcomeCommitted   = api.CommitOutcomeCommitted
	CommitOutcomeGap         = api.CommitOutcomeGap
	CommitOutcomeForbidden   = api.CommitOutcomeForbidden
	CommitOutcomeIncomplete  = api.CommitOutcomeIncomplete
	CommitOutcomeMismatch    = api.CommitOutcomeMismatch
	CommitOutcomeUnavailable = api.CommitOutcomeUnavailable
)

// CommitOutcomeValues returns every member of CommitOutcome in declaration order, in a new slice.
func CommitOutcomeValues() []CommitOutcome { return api.CommitOutcomeValues() }

type CommitResult = api.CommitResult

type Dangling = api.Dangling

type Entry = api.Entry

type Evidence = api.Evidence

const (
	EvidenceHashed  = api.EvidenceHashed
	EvidenceNamed   = api.EvidenceNamed
	EvidenceForeign = api.EvidenceForeign
	EvidenceNone    = api.EvidenceNone
)

// EvidenceValues returns every member of Evidence in declaration order, in a new slice.
func EvidenceValues() []Evidence { return api.EvidenceValues() }

type Hold = api.Hold

type Holder = api.Holder

type InventoryOutcome = api.InventoryOutcome

const (
	InventoryOutcomePage        = api.InventoryOutcomePage
	InventoryOutcomeGap         = api.InventoryOutcomeGap
	InventoryOutcomeForbidden   = api.InventoryOutcomeForbidden
	InventoryOutcomeInvalid     = api.InventoryOutcomeInvalid
	InventoryOutcomeUnavailable = api.InventoryOutcomeUnavailable
)

// InventoryOutcomeValues returns every member of InventoryOutcome in declaration order, in a new slice.
func InventoryOutcomeValues() []InventoryOutcome { return api.InventoryOutcomeValues() }

type Lifetime = api.Lifetime

const (
	LifetimeUntilReleased = api.LifetimeUntilReleased
	LifetimeLease         = api.LifetimeLease
	LifetimeWhilePresent  = api.LifetimeWhilePresent
)

// LifetimeValues returns every member of Lifetime in declaration order, in a new slice.
func LifetimeValues() []Lifetime { return api.LifetimeValues() }

type ListingOutcome = api.ListingOutcome

const (
	ListingOutcomePage        = api.ListingOutcomePage
	ListingOutcomeGap         = api.ListingOutcomeGap
	ListingOutcomeForbidden   = api.ListingOutcomeForbidden
	ListingOutcomeInvalid     = api.ListingOutcomeInvalid
	ListingOutcomeUnavailable = api.ListingOutcomeUnavailable
)

// ListingOutcomeValues returns every member of ListingOutcome in declaration order, in a new slice.
func ListingOutcomeValues() []ListingOutcome { return api.ListingOutcomeValues() }

type ListingPage = api.ListingPage

type Manifest = api.Manifest

type Name = api.Name

type Object = api.Object

type OpenOutcome = api.OpenOutcome

const (
	OpenOutcomeOpened      = api.OpenOutcomeOpened
	OpenOutcomeNotFound    = api.OpenOutcomeNotFound
	OpenOutcomeForbidden   = api.OpenOutcomeForbidden
	OpenOutcomeInvalid     = api.OpenOutcomeInvalid
	OpenOutcomeUnsupported = api.OpenOutcomeUnsupported
	OpenOutcomeUnavailable = api.OpenOutcomeUnavailable
	OpenOutcomeExhausted   = api.OpenOutcomeExhausted
)

// OpenOutcomeValues returns every member of OpenOutcome in declaration order, in a new slice.
func OpenOutcomeValues() []OpenOutcome { return api.OpenOutcomeValues() }

type OpenResult = api.OpenResult

type Placement = api.Placement

const (
	PlacementLocal  = api.PlacementLocal
	PlacementRemote = api.PlacementRemote
)

// PlacementValues returns every member of Placement in declaration order, in a new slice.
func PlacementValues() []Placement { return api.PlacementValues() }

type ReadOutcome = api.ReadOutcome

const (
	ReadOutcomeData        = api.ReadOutcomeData
	ReadOutcomeGap         = api.ReadOutcomeGap
	ReadOutcomeForbidden   = api.ReadOutcomeForbidden
	ReadOutcomeInvalid     = api.ReadOutcomeInvalid
	ReadOutcomeUnavailable = api.ReadOutcomeUnavailable
	ReadOutcomeChanged     = api.ReadOutcomeChanged
)

// ReadOutcomeValues returns every member of ReadOutcome in declaration order, in a new slice.
func ReadOutcomeValues() []ReadOutcome { return api.ReadOutcomeValues() }

type ReadResult = api.ReadResult

type ServiceError = api.ServiceError

type ServiceErrorCode = api.ServiceErrorCode

const (
	ServiceErrorCodeHandlerError      = api.ServiceErrorCodeHandlerError
	ServiceErrorCodeInvalidResult     = api.ServiceErrorCodeInvalidResult
	ServiceErrorCodeUnknownVersion    = api.ServiceErrorCodeUnknownVersion
	ServiceErrorCodeUnknownService    = api.ServiceErrorCodeUnknownService
	ServiceErrorCodeUnknownMethod     = api.ServiceErrorCodeUnknownMethod
	ServiceErrorCodeWrongMode         = api.ServiceErrorCodeWrongMode
	ServiceErrorCodeCallerUnavailable = api.ServiceErrorCodeCallerUnavailable
	ServiceErrorCodeIdentityRequired  = api.ServiceErrorCodeIdentityRequired
)

// ServiceErrorCodeValues returns every member of ServiceErrorCode in declaration order, in a new slice.
func ServiceErrorCodeValues() []ServiceErrorCode { return api.ServiceErrorCodeValues() }

type StoreError = api.StoreError

type StoreErrorKind = api.StoreErrorKind

const (
	StoreErrorKindOther                   = api.StoreErrorKindOther
	StoreErrorKindMalformedIndex          = api.StoreErrorKindMalformedIndex
	StoreErrorKindUnreadableIndex         = api.StoreErrorKindUnreadableIndex
	StoreErrorKindUnreadableTree          = api.StoreErrorKindUnreadableTree
	StoreErrorKindUnreadableConfiguration = api.StoreErrorKindUnreadableConfiguration
)

// StoreErrorKindValues returns every member of StoreErrorKind in declaration order, in a new slice.
func StoreErrorKindValues() []StoreErrorKind { return api.StoreErrorKindValues() }

type Verification = api.Verification

const (
	VerificationUnverified = api.VerificationUnverified
)

// VerificationValues returns every member of Verification in declaration order, in a new slice.
func VerificationValues() []Verification { return api.VerificationValues() }
