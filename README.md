# BitTorrent Client

A BitTorrent client implementation in Go supporting both .torrent files and magnet links, built according to the [BitTorrent Protocol Specification](https://www.bittorrent.org/beps/bep_0003.html).

## Features
- Download torrents from .torrent files
- Magnet link support with metadata fetching
- Concurrent piece downloads
- Extension protocol support

## Usage
### Download with torrent file
./your_program download -o &lt;destination&gt; &lt;torrent file&gt;

### Download with magnet link
./your_program download_magnet -o &lt;destination&gt; &lt;magnet link&gt;
