# shellcheck shell=bash
# Build OxiPNG without a dependency on the separately installed Visual C++ runtime.
get https://static.crates.io/crates/oxipng/oxipng-10.2.1.crate \
  oxipng-10.2.1.crate 52a88e3e5c2ad82e691b219e2b45a90eb5a51d26a434c7e191563e957a48e07a
get https://static.rust-lang.org/dist/2026-10-15/rustc-1.99.1-x86_64-pc-windows-gnullvm.tar.xz \
  rustc-1.99.1-gnullvm.tar.xz 00eb55f0c1fd330a0f7b82d288b32648b867bd523e55039d69c5914804dd7ad5
get https://static.rust-lang.org/dist/2026-10-15/cargo-1.99.1-x86_64-pc-windows-gnullvm.tar.xz \
  cargo-1.99.1-gnullvm.tar.xz 44753601667a1b201c9e2e4233d8311e2e7d3321273d6041a07e3d4e7966025b
get https://static.rust-lang.org/dist/2026-10-15/rust-std-1.99.1-x86_64-pc-windows-gnullvm.tar.xz \
  rust-std-1.99.1-gnullvm.tar.xz 8ddc26705e13c46530c898524b0d2657b7d416a25e8d9ffe369a97b23a619bf6
RUST=$D/rust-1.99.1-gnullvm
mkdir -p "$RUST" "$D/oxipng"
if [ ! -f "$RUST/.ready" ]; then
  for component in rustc cargo rust-std; do
    tar -xJf "$D/cache/$component-1.99.1-gnullvm.tar.xz" -C "$D/oxipng"
    content=$component
    if [ "$component" = rust-std ]; then content=rust-std-x86_64-pc-windows-gnullvm; fi
    cp -r "$D/oxipng/$component-1.99.1-x86_64-pc-windows-gnullvm/$content/." "$RUST/"
  done
  cp "$D/oxipng/rustc-1.99.1-x86_64-pc-windows-gnullvm/"LICENSE-* "$RUST/"
  touch "$RUST/.ready"
fi
if [ ! -f "$D/oxipng/oxipng-10.2.1/Cargo.toml" ]; then
  tar -xzf "$D/cache/oxipng-10.2.1.crate" -C "$D/oxipng"
fi
(
  export PATH="$RUST/bin:$PATH"
  export CARGO_HOME="$D/oxipng/cargo" CARGO_TARGET_DIR="$D/oxipng/target"
  export CARGO_TARGET_X86_64_PC_WINDOWS_GNULLVM_LINKER=clang CC=clang AR=llvm-ar
  export RUSTFLAGS='-C target-feature=+crt-static'
  cargo build --manifest-path "$D/oxipng/oxipng-10.2.1/Cargo.toml" --locked --release --target x86_64-pc-windows-gnullvm
  cargo metadata --manifest-path "$D/oxipng/oxipng-10.2.1/Cargo.toml" --locked --filter-platform x86_64-pc-windows-gnullvm --format-version 1 >"$D/oxipng/metadata.json"
)
cp "$D/oxipng/target/x86_64-pc-windows-gnullvm/release/oxipng.exe" "$RT/compress/"
llvm_licence=$(dirname "$(dirname "$(command -v clang)")")/LICENSE.TXT
python "$ROOT/scripts/oxipng-notices.py" "$D/oxipng/metadata.json" "$RUST" "$llvm_licence" "$D/licenses/oxipng.LICENSE.txt"
