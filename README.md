<img src="https://quantkiosk.com/assets/img/qk-logo.png" height="40" />

## The Scheduler for Devs, Not DevOps

>[!IMPORTANT]
>
> This repo is currently a snapshot of primary source. It may not reflect all development
> until it becomes the primary.
>
> Dual license under AGPL-3 and Commercial.
>
> Contact jeff@rpeat.io for additional information or to discuss commercial options
> including alternative site licensing, hosting and support.
>
> Visit us at [rpeat.io](https://rpeat.io) and [rpeat.io/docs](https://rpeat.io/docs)


**rpeat** was born out of necessity. In a heterogenous world, where no single language
rules them all, we needed a way to run everything from servers to scripts in one place.
The mandate was simple: language agnostic, dead simple to use, and enough functions to
get the job done, without being complicated to set up or use.


Welcome to your new scheduler.

## Key Features
* Single binary server - no dependencies, fully compiled, cross platform
* Intentionally lightweight, it utilizes the GO programming language to full effect
* Designed to be like cron, but 50 years newer.
* Easy install (just download) and you can run as many instances as needed. No limits.
* No need to run a database, install any other software or learn much of anything.

## Outstanding Features

## rpeat-server
This is where "jobs" live and run. A job is nothing more than a commands to run
some user-defined specs, and a name.

* Browser-based GUI for monitoring and control
* Self-contained binary
* Config driven - XML or JSON
* Full API access for use *within* code if needed.
* Granular permissioning at job level
* Env variables
* Magic DateEnv variables (e.g. CCYY-MM -> 20225-11)
* Timezones
* Custom calendars
* One-second triggers
* Extended cron-syntax support including disjoint triggers
* Start, stop, end and restart triggers.
* Graph-based dependency triggers based on state
* Retries
* Automatic logging (including rotation)
* Alerts via SMTP (e.g. gmail or outlook)
* Templates - reuse code, environments, etc
* Themes
* TLS/SSL
* Built in demo to get started.
* and more!

## rpeat-util
Unlike many other tools - rpeat was designed for the rigorous demands
of running a hedge fund. This means correctness and transparency is paramount.
We build an additional set of utilities to allow for validation and testing
configurations before running.

### validate
Run a series of tests against your configuration and reports any warnings or errors. These tests covers malformed schedules, missing calendars, incorrect timezones, missing dependencies, bad permissions - over 15 different factors
### date
Test various options of dynamic (magic) DateEnv to calculate things like one quarter from today, or three days back using Monday to Friday calendar.
### next
Calculate *next* trigger events taking into account timezones and calendars.
### convert
Convert between XML and JSON job files
### jobstate
Interogate the on-disk state
