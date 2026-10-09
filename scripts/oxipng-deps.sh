# shellcheck shell=bash
# Build OxiPNG without a dependency on the separately installed Visual C++ runtime.
get https://static.crates.io/crates/oxipng/oxipng-9.1.5.crate \
  oxipng-9.1.5.crate 26c613f0f566526a647c7473f6a8556dbce22c91b13485ee4b4ec7ab648e4973
get https://static.rust-lang.org/dist/2026-10-01/rustc-1.99.0-x86_64-pc-windows-gnullvm.tar.xz \
  rustc-1.99.0-gnullvm.tar.xz be5384e6709f884de1f16c4588baa621707be12531b5206956d5b58ceb9ff5dd
get https://static.rust-lang.org/dist/2026-10-01/cargo-1.99.0-x86_64-pc-windows-gnullvm.tar.xz \
  cargo-1.99.0-gnullvm.tar.xz 88e0f9be673fb07b5bb13b4a0992c3a5e1f7f0d363d74fdffd16a0d001dff447
get https://static.rust-lang.org/dist/2026-10-01/rust-std-1.99.0-x86_64-pc-windows-gnullvm.tar.xz \
  rust-std-1.99.0-gnullvm.tar.xz dcf636d91ef0639dd31a67169c7d7e27e1f013632490e1af97753b31728e85c0
RUST=$D/rust-1.99.0-gnullvm
mkdir -p "$RUST" "$D/oxipng"
if [ ! -f "$RUST/.ready" ]; then
  for component in rustc cargo rust-std; do
    tar -xJf "$D/cache/$component-1.99.0-gnullvm.tar.xz" -C "$D/oxipng"
    content=$component
    if [ "$component" = rust-std ]; then content=rust-std-x86_64-pc-windows-gnullvm; fi
    cp -r "$D/oxipng/$component-1.99.0-x86_64-pc-windows-gnullvm/$content/." "$RUST/"
  done
  cp "$D/oxipng/rustc-1.99.0-x86_64-pc-windows-gnullvm/"LICENSE-* "$RUST/"
  touch "$RUST/.ready"
fi
if [ ! -f "$D/oxipng/oxipng-9.1.5/Cargo.toml" ]; then
  tar -xzf "$D/cache/oxipng-9.1.5.crate" -C "$D/oxipng"
fi
(
  export PATH="$RUST/bin:$PATH"
  export CARGO_HOME="$D/oxipng/cargo" CARGO_TARGET_DIR="$D/oxipng/target"
  export CARGO_TARGET_X86_64_PC_WINDOWS_GNULLVM_LINKER=clang CC=clang AR=llvm-ar
  # libdeflate 1.23 uses the removed evex512 target attribute on recent clang.
  # Keep its portable SSE/AVX2 dispatch without changing the locked sources.
  export CFLAGS='-DLIBDEFLATE_ASSEMBLER_DOES_NOT_SUPPORT_AVX512VNNI -DLIBDEFLATE_ASSEMBLER_DOES_NOT_SUPPORT_VPCLMULQDQ'
  export RUSTFLAGS='-C target-feature=+crt-static'
  cargo build --manifest-path "$D/oxipng/oxipng-9.1.5/Cargo.toml" --locked --release --target x86_64-pc-windows-gnullvm
  cargo metadata --manifest-path "$D/oxipng/oxipng-9.1.5/Cargo.toml" --locked --filter-platform x86_64-pc-windows-gnullvm --format-version 1 >"$D/oxipng/metadata.json"
)
cp "$D/oxipng/target/x86_64-pc-windows-gnullvm/release/oxipng.exe" "$RT/compress/"
llvm_licence=$(dirname "$(dirname "$(command -v clang)")")/LICENSE.TXT
python "$ROOT/scripts/oxipng-notices.py" "$D/oxipng/metadata.json" "$RUST" "$llvm_licence" "$D/licenses/oxipng.LICENSE.txt"
