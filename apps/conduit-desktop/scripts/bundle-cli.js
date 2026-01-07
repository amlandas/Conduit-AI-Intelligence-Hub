/**
 * Bundle CLI Script
 *
 * Compiles Go binaries (conduit, conduit-daemon) and copies them to the
 * app's resources folder for distribution with the DMG.
 *
 * Usage: npm run bundle:cli
 */

const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');
const os = require('os');

// Paths
const ROOT_DIR = path.resolve(__dirname, '../../..');
const RESOURCES_DIR = path.resolve(__dirname, '../resources/bin');
const PACKAGE_JSON = path.resolve(__dirname, '../package.json');

// Get package.json for version info
const pkg = JSON.parse(fs.readFileSync(PACKAGE_JSON, 'utf-8'));

// Get actual CLI version from git (like Makefile does)
let cliVersion;
try {
  cliVersion = execFileSync('git', ['describe', '--tags', '--always', '--dirty'], {
    cwd: ROOT_DIR,
    encoding: 'utf-8',
  }).trim();
} catch {
  cliVersion = pkg.conduit?.bundledCLIVersion || '0.1.0';
}
const buildTime = new Date().toISOString();

// Detect architecture
const arch = process.arch === 'arm64' ? 'arm64' : 'amd64';
const platform = os.platform();

console.log('');
console.log('='.repeat(60));
console.log('  Conduit CLI Bundler');
console.log('='.repeat(60));
console.log(`  Platform: ${platform}`);
console.log(`  Architecture: ${arch}`);
console.log(`  CLI Version: ${cliVersion}`);
console.log(`  Build Time: ${buildTime}`);
console.log(`  Output: ${RESOURCES_DIR}`);
console.log('='.repeat(60));
console.log('');

// Ensure resources/bin directory exists
if (!fs.existsSync(RESOURCES_DIR)) {
  fs.mkdirSync(RESOURCES_DIR, { recursive: true });
  console.log(`Created directory: ${RESOURCES_DIR}`);
}

// Build environment
const buildEnv = {
  ...process.env,
  CGO_ENABLED: '1',
  GOOS: 'darwin',
  GOARCH: arch,
};

// ldflags for version injection (matching Makefile format)
const ldflags = `-s -w -X main.Version=${cliVersion} -X main.BuildTime=${buildTime}`;

// Build conduit CLI using execFileSync (safer than execSync)
console.log('Building conduit CLI...');
try {
  execFileSync('go', [
    'build',
    '-tags', 'fts5',
    '-trimpath',
    '-ldflags', ldflags,
    '-o', path.join(RESOURCES_DIR, 'conduit'),
    './cmd/conduit'
  ], {
    cwd: ROOT_DIR,
    env: buildEnv,
    stdio: 'inherit',
  });
  console.log('  conduit CLI built successfully');
} catch (error) {
  console.error('Failed to build conduit CLI:', error.message);
  process.exit(1);
}

// Build conduit-daemon using execFileSync (safer than execSync)
console.log('Building conduit-daemon...');
try {
  execFileSync('go', [
    'build',
    '-tags', 'fts5',
    '-trimpath',
    '-ldflags', ldflags,
    '-o', path.join(RESOURCES_DIR, 'conduit-daemon'),
    './cmd/conduit-daemon'
  ], {
    cwd: ROOT_DIR,
    env: buildEnv,
    stdio: 'inherit',
  });
  console.log('  conduit-daemon built successfully');
} catch (error) {
  console.error('Failed to build conduit-daemon:', error.message);
  process.exit(1);
}

// Make binaries executable
fs.chmodSync(path.join(RESOURCES_DIR, 'conduit'), 0o755);
fs.chmodSync(path.join(RESOURCES_DIR, 'conduit-daemon'), 0o755);

// Create version manifest
const manifest = {
  version: cliVersion,
  platform: platform,
  arch: arch,
  buildDate: buildTime,
  binaries: ['conduit', 'conduit-daemon'],
};

fs.writeFileSync(
  path.join(RESOURCES_DIR, 'manifest.json'),
  JSON.stringify(manifest, null, 2)
);

console.log('');
console.log('Bundle complete!');
console.log(`  conduit: ${RESOURCES_DIR}/conduit`);
console.log(`  conduit-daemon: ${RESOURCES_DIR}/conduit-daemon`);
console.log(`  manifest: ${RESOURCES_DIR}/manifest.json`);
console.log('');

// Show file sizes
const conduitSize = fs.statSync(path.join(RESOURCES_DIR, 'conduit')).size;
const daemonSize = fs.statSync(path.join(RESOURCES_DIR, 'conduit-daemon')).size;
console.log(`File sizes:`);
console.log(`  conduit: ${(conduitSize / 1024 / 1024).toFixed(2)} MB`);
console.log(`  conduit-daemon: ${(daemonSize / 1024 / 1024).toFixed(2)} MB`);
console.log('');
