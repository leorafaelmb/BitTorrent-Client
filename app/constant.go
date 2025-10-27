package main

// Peer ID used for all BitTorrent communications
const PeerID = "leofeopeoluvsanayeli"

// BitTorrent Protocol
const (
	ProtocolString       = "BitTorrent protocol"
	ProtocolStringLength = 19
	HandshakeLength      = 68
)

// Message IDs
const (
	MessageChoke         byte = 0
	MessageUnchoke       byte = 1
	MessageInterested    byte = 2
	MessageNotInterested byte = 3
	MessageHave          byte = 4
	MessageBitfield      byte = 5
	MessageRequest       byte = 6
	MessagePiece         byte = 7
	MessageCancel        byte = 8
	MessageExtension     byte = 20 // BEP 10 Extension Protocol
)

// Download config
const (
	MaxPipelineRequests int    = 5       // Maximum concurrent block requests per peer
	BlockSize           uint32 = 1 << 14 // 16KB - standard block size
	MetadataPieceSize          = 1 << 14 // 16KB - metadata piece size for magnet links
)

// Network config
const (
	DefaultPort       = 6881
	DefaultUploaded   = 0
	DefaultDownloaded = 0
	DefaultCompact    = 1
	ConnectionTimeout = 3 // seconds
)

// Magnet Link Extension
const (
	ExtensionBitPosition = 5 // Reserved byte index for extension bit
	ExtensionID          = 0x10
)
