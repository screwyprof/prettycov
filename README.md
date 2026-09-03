# Prettycov
[![codecov](https://codecov.io/gh/screwyprof/prettycov/graph/badge.svg)](https://codecov.io/gh/screwyprof/prettycov) [![Go](https://github.com/screwyprof/prettycov/actions/workflows/go.yml/badge.svg)](https://github.com/screwyprof/prettycov/actions/workflows/go.yml)<!-- ALL-CONTRIBUTORS-BADGE:START - Do not remove or modify this section -->
[![All Contributors](https://img.shields.io/badge/all_contributors-1-orange.svg?style=flat-square)](#contributors-)
<!-- ALL-CONTRIBUTORS-BADGE:END --> 

Pretty Golang Coverage.

The other day I wanted to output a pretty overall coverage summary in my terminal.
I wanted to show a table or a tree with top-level packages and their corresponding coverage. 
I tried to search for some ready to use tools which would offer something similar but with not luck.
After that, I decided to build it on my own. So here it is :)

## Installation
```shell
go install github.com/screwyprof/prettycov/cmd/prettycov@latest
```

## How to use
### Getting built-in help
Run `prettycov help`, `prettycov -help` or `prettycov -h` for the built-in usage info. Bare `prettycov` reads `./coverage.out` and prints the report.

### Run your tests with coverage
`prettycov` works by parsing coverage profile, so the first thing to do is to run tests with coverage:

`go test -cover -coverprofile=coverage.out` ./...

### Show coverage summary up to the given depth
The profile defaults to `./coverage.out`. Name another one positionally (`prettycov path/to/cov.out`) or with `-profile`.

You may also specify `-depth` to set how many levels to show below the top row, the way `tree -L` counts them. Set it past the depth of the tree to drill all the way down and find what is dragging coverage:

```shell
❯ prettycov -depth=9
 github.com/screwyprof/delegator - 94.01
 ├ pkg - 96.41
 │ ├ clock - 100.00
 │ ├ httpkit - 97.50
 │ ├ logger - 96.88
 │ ├ pgxdb - 90.00
 │ └ tzkt - 100.00
 ├ scraper - 90.00
 │ ├ config - 100.00
 │ └ store - 77.97
 │   ├ dbrow - 100.00
 │   └ pgxstore - 75.47
 └ web - 95.15
   ├ api - 100.00
   ├ handler - 89.66
   │ └ bind - 85.71
   ├ store/pgxstore - 96.49
   └ tezos - 100.00
```

### Show coverage summary with replaced paths
Sometimes the project may have a long project path (package path to be more precise) which clutters the output. 
In this case you may want to replace it with a shorter name:

```shell
❯ prettycov -depth=2 -old github.com/screwyprof/delegator -new delegator
 delegator - 94.01
 ├ pkg - 96.41
 │ ├ clock - 100.00
 │ ├ httpkit - 97.50
 │ ├ logger - 96.88
 │ ├ pgxdb - 90.00
 │ └ tzkt - 100.00
 ├ scraper - 90.00
 │ ├ config - 100.00
 │ └ store - 77.97
 └ web - 95.15
   ├ api - 100.00
   ├ handler - 89.66
   ├ store/pgxstore - 96.49
   └ tezos - 100.00
```

### Getting top-level coverage info
This is what I created this tool for. You may get a nice top-level package coverage:

```shell
❯ prettycov
 github.com/screwyprof/delegator - 94.01
 ├ pkg - 96.41
 ├ scraper - 90.00
 └ web - 95.15
```

### Fail below a threshold
`-fail-under` exits 1 when total coverage is under the given percentage, so `prettycov` can gate CI. It stays distinct from exit 2, which means prettycov could not run at all:

```shell
❯ prettycov -fail-under=99
 github.com/screwyprof/delegator - 94.01
 ├ pkg - 96.41
 ├ scraper - 90.00
 └ web - 95.15
total coverage 94.01% is below 99.00%
❯ echo $?
1
```

### Colour
Percentages are graded red, yellow and green using only the base ANSI colours, so your own terminal theme decides the shades. Colour is on when writing to a terminal and off when piped, honouring [`NO_COLOR`](https://no-color.org) and `TERM=dumb`. Override with `-color=always` or `-color=never`.

## How it works
It parses the coverage profile to populate a prefix tree of paths and coverages.
Then it traverses the tree from the furthermost leaves to top merging the coverage info. 
Then it draws the top row plus `-depth` levels beneath it. A run of directories that each hold nothing but the next one renders as a single row.

### What's next?

#### Add tests and run `prettycov` in the CI to get the report for this project
This project was born spontaneously when I was on vacation. I didn't have much time, so just coded the idea with no tests.
Now that it proved to be useful it would be great to add tests. For now, I've got a red coverage badge to remind me about it.

## Contributors ✨
Thanks goes to these wonderful people ([emoji key](https://allcontributors.org/docs/en/emoji-key)):

<!-- ALL-CONTRIBUTORS-LIST:START - Do not remove or modify this section -->
<!-- prettier-ignore-start -->
<!-- markdownlint-disable -->
<table>
  <tbody>
    <tr>
      <td align="center"><a href="https://github.com/kannman"><img src="https://avatars.githubusercontent.com/u/40325995?v=4?s=100" width="100px;" alt=""/><br /><sub><b>antongr</b></sub></a><br /><a href="https://github.com/screwyprof/prettycov/commits?author=kannman" title="Code">💻</a></td>
    </tr>
  </tobdy>
</table>

<!-- markdownlint-restore -->
<!-- prettier-ignore-end -->

<!-- ALL-CONTRIBUTORS-LIST:END -->

This project follows the [all-contributors](https://github.com/all-contributors/all-contributors) specification. Contributions of any kind welcome!
