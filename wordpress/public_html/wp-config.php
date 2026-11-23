<?php
/**
 * The base configurations of the WordPress.
 *
 * This file has the following configurations: MySQL settings, Table Prefix,
 * Secret Keys, WordPress Language, and ABSPATH. You can find more information
 * by visiting {@link http://codex.wordpress.org/Editing_wp-config.php Editing
 * wp-config.php} Codex page. You can get the MySQL settings from your web host.
 *
 * This file is used by the wp-config.php creation script during the
 * installation. You don't have to use the web site, you can just copy this file
 * to "wp-config.php" and fill in the values.
 *
 * @package WordPress
 */

/**
 * Secrets come from the environment (wordpress/.env locally, see .env.example),
 * never from this file: it is in a public git repository.
 */
function olamundo_env($name) {
	$value = getenv($name);
	if ($value === false || $value === "") {
		http_response_code(500);
		die("Missing environment variable $name (see wordpress/.env.example)\n");
	}
	return $value;
}

// ** MySQL settings - You can get this info from your web host ** //
/** The name of the database for WordPress */

define('DB_NAME', olamundo_env('WORDPRESS_DB_NAME'));

/** MySQL database username */
define('DB_USER', olamundo_env('WORDPRESS_DB_USER'));


/** MySQL database password */
define('DB_PASSWORD', olamundo_env('WORDPRESS_DB_PASSWORD'));

/** MySQL hostname */
define('DB_HOST', olamundo_env('WORDPRESS_DB_HOST'));

/** Database Charset to use in creating database tables. */
define('DB_CHARSET', 'utf8');

/** The Database Collate type. Don't change this if in doubt. */
define('DB_COLLATE', '');

/** logowanie i admin przez ssl */
// define('FORCE_SSL_LOGIN', true);
// define('FORCE_SSL_ADMIN', true);


/**#@+
 * Authentication Unique Keys and Salts.
 *
 * Change these to different unique phrases!
 * You can generate these using the {@link https://api.wordpress.org/secret-key/1.1/salt/ WordPress.org secret-key service}
 * You can change these at any point in time to invalidate all existing cookies. This will force all users to have to log in again.
 *
 * @since 2.6.0
 */
define('AUTH_KEY',         olamundo_env('WORDPRESS_AUTH_KEY'));
define('SECURE_AUTH_KEY',  olamundo_env('WORDPRESS_SECURE_AUTH_KEY'));
define('LOGGED_IN_KEY',    olamundo_env('WORDPRESS_LOGGED_IN_KEY'));
define('NONCE_KEY',        olamundo_env('WORDPRESS_NONCE_KEY'));
define('AUTH_SALT',        olamundo_env('WORDPRESS_AUTH_SALT'));
define('SECURE_AUTH_SALT', olamundo_env('WORDPRESS_SECURE_AUTH_SALT'));
define('LOGGED_IN_SALT',   olamundo_env('WORDPRESS_LOGGED_IN_SALT'));
define('NONCE_SALT',       olamundo_env('WORDPRESS_NONCE_SALT'));

/**
 * Other customizations.
 */
define('FS_METHOD','direct');define('FS_CHMOD_DIR',0755);define('FS_CHMOD_FILE',0644);
define('WP_POST_REVISIONS', 3);   // keep only the 3 newest revisions per post

/**
 * Turn off automatic updates since these are managed upstream.
 */
define('AUTOMATIC_UPDATER_DISABLED', true);


/**#@-*/

/**
 * WordPress Database Table prefix.
 *
 * You can have multiple installations in one database if you give each a unique
 * prefix. Only numbers, letters, and underscores please!
 */
$table_prefix  = 'wp_';

/**
 * WordPress Localized Language, defaults to English.
 *
 * Change this to localize WordPress. A corresponding MO file for the chosen
 * language must be installed to wp-content/languages. For example, install
 * de_DE.mo to wp-content/languages and set WPLANG to 'de_DE' to enable German
 * language support.
 */
define('WPLANG', 'pl_PL');

/**
 * For developers: WordPress debugging mode.
 *
 * Change this to true to enable the display of notices during development.
 * It is strongly recommended that plugin and theme developers use WP_DEBUG
 * in their development environments.
 */
define('WP_DEBUG', false);

/* That's all, stop editing! Happy blogging. */

/** Absolute path to the WordPress directory. */
if ( !defined('ABSPATH') )
	define('ABSPATH', dirname(__FILE__) . '/');

/** Sets up WordPress vars and included files. */
require_once(ABSPATH . 'wp-settings.php');
