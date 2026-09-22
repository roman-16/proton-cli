package kit

// The flag vocabulary.
//
// Verbs are declared, argument names are declared, and for a long time flags
// were not - so a name got in by being spelled, and a second meaning got in by
// being spelled the same. That is how --color came to take an accent name on ten
// commands and a bare integer on one, and how --start came to mean both an
// event's own beginning and the first day of a query.
//
// So a flag used by more than one command is declared here, exactly as a verb
// is: one entry, one meaning, and - where its value names something this CLI
// holds - the collection it names, which is what shell completion answers from.
//
// The conformance test reads this in both directions. A shared name missing here
// fails, and an entry nothing uses fails too, because a vocabulary that keeps
// words after they stop being spoken stops describing the CLI.

// None is what a flag takes to mean "nothing", where the values it otherwise
// accepts have a shape the word cannot be mistaken for.
//
// An address is such a value: no address is spelled `none`, so the flag that
// sets one can say it, and a --clear-x beside it would be a second way to say
// the same thing. Free text and a secret read from a file are the cases that
// cannot, and those keep their --clear-x.
const None = "none"

// Flag is one flag name: what it means, and what its value names.
type Flag struct {
	// Means is the sentence that explains the name. It is the meaning, not the
	// wording: "Set the postal address" on a create and "Replace the postal
	// address" on the update beside it are one meaning.
	Means string
	// Picks is the collection whose things the value names, for the flags whose
	// value is a reference. It is a command line, so completion asks the same
	// cache an argument asks. It is empty for a value that names something this
	// CLI does not hold - a local path, an address, a duration, a free word - and
	// such a value is left to the shell or to the flag's own declared domain.
	Picks string
}

// Flags is every flag name used by more than one command.
var Flags = map[string]Flag{
	"access":                 {Means: "what somebody may do with a shared thing"},
	"address":                {Means: "a postal or street address"},
	"after":                  {Means: "the first day a selection includes"},
	"album":                  {Means: "the photo album to act in", Picks: "drive photos albums"},
	"all":                    {Means: "act on everything in the command's scope, rather than a subset"},
	"all-day":                {Means: "an event with no time of day"},
	"anniversary":            {Means: "a date being commemorated"},
	"answer":                 {Means: "a reply to an invitation"},
	"attach":                 {Means: "a file to attach"},
	"attach-inline":          {Means: "an image to embed in an HTML body by Content-ID"},
	"attendee":               {Means: "someone invited"},
	"bcc":                    {Means: "a blind-carbon-copy recipient"},
	"before":                 {Means: "the last day a selection includes"},
	"birthdate":              {Means: "a date of birth"},
	"birthday":               {Means: "a date of birth"},
	"body":                   {Means: "the text a message carries"},
	"body-only":              {Means: "emit only the body"},
	"calendar":               {Means: "the calendar to act in", Picks: "calendar settings calendars"},
	"catch-all":              {Means: "where mail sent to a name the domain has not got arrives, or none"},
	"cc":                     {Means: "a carbon-copy recipient"},
	"city":                   {Means: "a city"},
	"clear-link-password":    {Means: "take the password off a public link"},
	"clear-name":             {Means: "remove the name"},
	"clear-signature":        {Means: "remove the signature"},
	"code":                   {Means: "the code Proton sent to an address or a phone"},
	"color":                  {Means: "the colour to set, by name"},
	"company":                {Means: "the company somebody works for"},
	"computer":               {Means: "the computer whose files to act in", Picks: "drive computers"},
	"country":                {Means: "a country"},
	"county":                 {Means: "an administrative county"},
	"days":                   {Means: "the days a schedule is active"},
	"default":                {Means: "make it what a new alias gets when nothing names another"},
	"delete-photos":          {Means: "also remove the photos an album held"},
	"desc":                   {Means: "reverse the order a listing is in"},
	"description":            {Means: "free-text description"},
	"detailed":               {Means: "also record the IP address of each event"},
	"dest":                   {Means: "the local path to write the payload to; - is stdout"},
	"dest-dir":               {Means: "a local directory to fill, keeping each item's own name"},
	"detach":                 {Means: "an attachment to remove"},
	"display-name":           {Means: "the name recipients see"},
	"draft":                  {Means: "save instead of sending"},
	"duration":               {Means: "how long something lasts"},
	"email":                  {Means: "an email address"},
	"eml":                    {Means: "an RFC 822 file to build the message from"},
	"end":                    {Means: "the end of the thing being described"},
	"eo-password-file":       {Means: "where to read the password for recipients outside Proton from; - is stdin"},
	"eo-password-hint":       {Means: "hint shown to password-protected recipients"},
	"everyone":               {Means: "answer every address that was on the message, not only the sender"},
	"expires":                {Means: "how long before it stops working, or never"},
	"expiry":                 {Means: "a payment card expiry date"},
	"extra-password-file":    {Means: "where to read the Pass extra password from; - is stdin"},
	"facebook":               {Means: "a Facebook handle"},
	"field":                  {Means: "a custom field, as NAME=VALUE"},
	"first-name":             {Means: "a given name"},
	"floor":                  {Means: "a floor within a building"},
	"folder":                 {Means: "the mail location to look in", Picks: "mail settings folders"},
	"force":                  {Means: "overwrite what is already there"},
	"format":                 {Means: "the file layout to write"},
	"from":                   {Means: "the sender: compose sets it, a filter matches it"},
	"full-name":              {Means: "a full name"},
	"gender":                 {Means: "a gender"},
	"generate-password":      {Means: "make the password rather than being given one"},
	"holder":                 {Means: "the name on a payment card"},
	"html":                   {Means: "treat the text as HTML rather than escaping it"},
	"icon":                   {Means: "the icon to set, by name"},
	"if":                     {Means: "a condition matching mail must meet"},
	"include-inline":         {Means: "include inline attachments"},
	"instagram":              {Means: "an Instagram handle"},
	"into":                   {Means: "the remote container something is put in"},
	"job-title":              {Means: "a job title"},
	"key":                    {Means: "an armoured PGP key"},
	"keyword":                {Means: "full-text search term"},
	"label":                  {Means: "the label to attach or detach", Picks: "mail settings labels"},
	"language":               {Means: "a preferred language"},
	"larger-than":            {Means: "select files above a size"},
	"last-name":              {Means: "a family name"},
	"length":                 {Means: "how many characters a generated password has"},
	"license-number":         {Means: "a driving licence number"},
	"limit":                  {Means: "how many things an answer holds or a verb acts on; 0 for all of them"},
	"link":                   {Means: "the public link somebody sent you to act in"},
	"link-password-file":     {Means: "where to read a public link's password from; - is stdin"},
	"linkedin":               {Means: "a LinkedIn handle"},
	"location":               {Means: "where something is"},
	"mailbox":                {Means: "where mail to an alias should arrive", Picks: "pass settings mailboxes"},
	"manager":                {Means: "the password manager that wrote a file being read in"},
	"mark-read":              {Means: "mark matching mail as read"},
	"match":                  {Means: "whether every condition must hold or any one of them"},
	"message":                {Means: "an accompanying note"},
	"middle-name":            {Means: "a middle name"},
	"name":                   {Means: "the name to set"},
	"new-password-file":      {Means: "where to read the password being set from; - is stdin"},
	"newer-than":             {Means: "select things newer than a duration"},
	"nickname":               {Means: "a familiar name"},
	"no-attachments":         {Means: "leave attachments out"},
	"no-digits":              {Means: "leave the digits out of a generated password"},
	"no-quote":               {Means: "do not quote the message being answered"},
	"no-remind":              {Means: "leave an event with no reminder"},
	"no-signature":           {Means: "leave the signature out"},
	"no-symbols":             {Means: "leave the symbols out of a generated password"},
	"no-uppercase":           {Means: "leave the capitals out of a generated password"},
	"note":                   {Means: "free-text note"},
	"notify":                 {Means: "tell you when mail arrives in a folder"},
	"older-than":             {Means: "select things older than a duration"},
	"onwards":                {Means: "extend the change to every later occurrence of a series"},
	"organization":           {Means: "an organization name"},
	"others":                 {Means: "act on every session but this one"},
	"page":                   {Means: "which page of results"},
	"parent":                 {Means: "the containing folder", Picks: "mail settings folders"},
	"passphrase-file":        {Means: "where to read the passphrase that locks a file; - is stdin"},
	"passport-number":        {Means: "a passport number"},
	"password-file":          {Means: "where to read the account password from; - is stdin"},
	"pattern":                {Means: "select by glob against the name"},
	"personal-website":       {Means: "a personal website, as opposed to a work one"},
	"phone":                  {Means: "a phone number"},
	"postal-code":            {Means: "a postal code"},
	"prefix":                 {Means: "the local part of an alias"},
	"public-key":             {Means: "a public key"},
	"purge":                  {Means: "also remove local data"},
	"query":                  {Means: "a URL query parameter"},
	"recursive":              {Means: "descend into subdirectories"},
	"reddit":                 {Means: "a Reddit handle"},
	"remind":                 {Means: "a reminder before the start"},
	"removed":                {Means: "list what was taken away rather than what is there"},
	"render":                 {Means: "which representation of a message body to print"},
	"repeat":                 {Means: "how a schedule repeats"},
	"revoke":                 {Means: "also invalidate the session at Proton"},
	"risk":                   {Means: "which password-health check a login fails"},
	"role":                   {Means: "the part somebody plays in an organization"},
	"rrule":                  {Means: "an iCalendar recurrence rule"},
	"scope":                  {Means: "the Drive subtree to look in"},
	"second-phone":           {Means: "a second phone number"},
	"secret-file":            {Means: "where to read a secret field from, as NAME=FILE; - is stdin"},
	"security":               {Means: "a Wi-Fi security protocol"},
	"send-at":                {Means: "when to deliver"},
	"separator":              {Means: "what stands between the words of a passphrase"},
	"shared":                 {Means: "the item somebody shared with you to act in", Picks: "drive shared"},
	"sieve":                  {Means: "a Sieve script"},
	"smaller-than":           {Means: "select files below a size"},
	"social-security-number": {Means: "a social security number"},
	"sort":                   {Means: "which key a listing is ordered by"},
	"ssid":                   {Means: "a Wi-Fi network name"},
	"star":                   {Means: "star matching mail"},
	"starred":                {Means: "select starred things"},
	"start":                  {Means: "the beginning of the thing being described"},
	"state":                  {Means: "a state or province"},
	"status":                 {Means: "whether an event is going ahead: confirmed, tentative or cancelled"},
	"strip-quotes":           {Means: "drop quoted reply blocks"},
	"subject":                {Means: "the subject line: compose sets it, a filter matches it"},
	"suffix":                 {Means: "the domain part of an alias", Picks: "pass settings domains"},
	"summary":                {Means: "one line per item instead of the whole thing"},
	"tag":                    {Means: "the photo tag to select"},
	"timezone":               {Means: "an IANA time zone"},
	"title":                  {Means: "a title: an event's, or a person's job title"},
	"to":                     {Means: "an email recipient: compose sets one, a filter matches one"},
	"totp":                   {Means: "a two-factor code"},
	"type":                   {Means: "the kind of thing to create or select"},
	"unread":                 {Means: "select unread things"},
	"until":                  {Means: "where a range stops"},
	"url":                    {Means: "a URL"},
	"username":               {Means: "a login username"},
	"vault":                  {Means: "the Pass vault to act in", Picks: "pass vaults"},
	"views":                  {Means: "how many openings a link allows"},
	"website":                {Means: "a website address"},
	"words":                  {Means: "how many words a passphrase has, instead of a password"},
	"work-email":             {Means: "a work email address"},
	"work-phone":             {Means: "a work phone number"},
	"x-handle":               {Means: "an X handle"},
	"yahoo":                  {Means: "a Yahoo handle"},
	"yes":                    {Means: "proceed without asking"},
	"zone":                   {Means: "an IANA time zone"},
}
