/**
 * Notarization script for macOS
 * 
 * This script is called after code signing by electron-builder.
 * It submits the app to Apple for notarization.
 * 
 * Required environment variables:
 * - APPLE_ID: Your Apple ID email
 * - APPLE_APP_SPECIFIC_PASSWORD: App-specific password from appleid.apple.com
 * - APPLE_TEAM_ID: Your Apple Developer Team ID
 */

const { notarize } = require('@electron/notarize');
const path = require('path');

exports.default = async function notarizing(context) {
  const { electronPlatformName, appOutDir } = context;

  // Only notarize on macOS
  if (electronPlatformName !== 'darwin') {
    console.log('Skipping notarization: not macOS');
    return;
  }

  // Check for required environment variables
  const appleId = process.env.APPLE_ID;
  const appleIdPassword = process.env.APPLE_APP_SPECIFIC_PASSWORD;
  const teamId = process.env.APPLE_TEAM_ID;

  if (!appleId || !appleIdPassword || !teamId) {
    console.log('Skipping notarization: missing Apple credentials');
    console.log('Set APPLE_ID, APPLE_APP_SPECIFIC_PASSWORD, and APPLE_TEAM_ID to enable notarization');
    return;
  }

  const appName = context.packager.appInfo.productFilename;
  const appPath = path.join(appOutDir, `${appName}.app`);

  console.log(`Notarizing ${appPath}...`);

  try {
    await notarize({
      appPath,
      appleId,
      appleIdPassword,
      teamId,
    });
    console.log('Notarization complete!');
  } catch (error) {
    console.error('Notarization failed:', error);
    throw error;
  }
};
