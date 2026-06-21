package dev.meshwork.cli

data class CliPositionalSpec(
    val name: String,
    val description: String,
)

data class CliOptionSpec(
    val longName: String,
    val shortName: String? = null,
    val valueName: String? = null,
    val description: String,
)

data class CliCommandSpec(
    val name: String,
    val summary: String,
    val positionals: List<CliPositionalSpec> = emptyList(),
    val options: List<CliOptionSpec> = emptyList(),
)

data class ParsedCliCommand(
    val command: CliCommandSpec,
    val positionalValues: Map<String, String>,
    val optionValues: Map<String, List<String>>,
) {
    fun positional(name: String): String = positionalValues.getValue(name)

    fun hasOption(longName: String): Boolean = optionValues.containsKey(longName)

    fun optionValue(longName: String): String? = optionValues[longName]?.lastOrNull()
}

sealed interface CliParseResult {
    data class Help(
        val text: String,
        val exitCode: Int,
    ) : CliParseResult

    data class Command(
        val parsed: ParsedCliCommand,
    ) : CliParseResult
}

class CliParser(
    private val programName: String,
    private val summary: String,
    private val commands: List<CliCommandSpec>,
) {
    private val commandByName = commands.associateBy { it.name }

    fun parse(args: Array<String>): CliParseResult {
        if (args.isEmpty()) {
            return CliParseResult.Help(renderGeneralHelp(), exitCode = 1)
        }
        if (args.size == 1 && isHelpToken(args[0])) {
            return CliParseResult.Help(renderGeneralHelp(), exitCode = 0)
        }

        val commandName = args.first()
        val command =
            commandByName[commandName]
                ?: throw IllegalArgumentException("Unknown command '$commandName'.${lineBreak()}${lineBreak()}${renderGeneralHelp()}")

        if (args.drop(1).any(::isHelpToken)) {
            return CliParseResult.Help(renderCommandHelp(command), exitCode = 0)
        }

        return CliParseResult.Command(parseCommand(command, args.drop(1)))
    }

    private fun parseCommand(
        command: CliCommandSpec,
        rawArgs: List<String>,
    ): ParsedCliCommand {
        val positionals = linkedMapOf<String, String>()
        val optionValues = linkedMapOf<String, MutableList<String>>()
        val optionByToken =
            command.options.flatMap { option ->
                listOfNotNull(option.longName, option.shortName).map { token -> token to option }
            }.toMap()

        var positionalIndex = 0
        var index = 0
        while (index < rawArgs.size) {
            val token = rawArgs[index]
            if (token.startsWith("-")) {
                val option =
                    optionByToken[token]
                        ?: throw IllegalArgumentException("Unknown option '$token'.${lineBreak()}${lineBreak()}${renderCommandHelp(command)}")
                if (option.valueName == null) {
                    optionValues.getOrPut(option.longName) { mutableListOf() }
                    index += 1
                    continue
                }

                require(index + 1 < rawArgs.size) {
                    "Expected ${option.valueName} after ${renderOptionNames(option)}.${lineBreak()}${lineBreak()}${renderCommandHelp(command)}"
                }
                optionValues.getOrPut(option.longName) { mutableListOf() }.add(rawArgs[index + 1])
                index += 2
                continue
            }

            if (positionalIndex >= command.positionals.size) {
                throw IllegalArgumentException("Unexpected argument '$token'.${lineBreak()}${lineBreak()}${renderCommandHelp(command)}")
            }
            positionals[command.positionals[positionalIndex].name] = token
            positionalIndex += 1
            index += 1
        }

        if (positionalIndex < command.positionals.size) {
            val missing = command.positionals[positionalIndex].name
            throw IllegalArgumentException("Missing required argument <$missing>.${lineBreak()}${lineBreak()}${renderCommandHelp(command)}")
        }

        return ParsedCliCommand(command, positionals, optionValues)
    }

    private fun renderGeneralHelp(): String =
        buildString {
            appendLine(programName)
            appendLine(summary)
            appendLine()
            appendLine("Usage:")
            appendLine("  $programName <command> [options]")
            appendLine()
            appendLine("Commands:")
            commands.forEach { command ->
                appendLine("  ${commandUsage(command).padEnd(30)} ${command.summary}")
            }
            appendLine()
            appendLine("Global options:")
            appendLine("  -h, --help".padEnd(30) + "Show help.")
            appendLine()
            append("Run '$programName <command> --help' for command details.")
        }.trimEnd()

    private fun renderCommandHelp(command: CliCommandSpec): String =
        buildString {
            appendLine(command.name)
            appendLine(command.summary)
            appendLine()
            appendLine("Usage:")
            appendLine("  $programName ${commandUsage(command)}")
            if (command.positionals.isNotEmpty()) {
                appendLine()
                appendLine("Arguments:")
                command.positionals.forEach { positional ->
                    appendLine("  <${positional.name}>".padEnd(30) + positional.description)
                }
            }
            appendLine()
            appendLine("Options:")
            command.options.forEach { option ->
                appendLine("  ${renderOptionUsage(option)}".padEnd(30) + option.description)
            }
            appendLine("  -h, --help".padEnd(30) + "Show help.")
        }.trimEnd()

    private fun commandUsage(command: CliCommandSpec): String =
        buildString {
            append(command.name)
            command.positionals.forEach { append(" <${it.name}>") }
            if (command.options.isNotEmpty()) {
                append(" [options]")
            }
        }

    private fun renderOptionUsage(option: CliOptionSpec): String =
        buildString {
            append(renderOptionNames(option))
            option.valueName?.let { append(" $it") }
        }

    private fun renderOptionNames(option: CliOptionSpec): String =
        listOfNotNull(option.shortName, option.longName).joinToString(", ")

    private fun isHelpToken(token: String): Boolean = token == "--help" || token == "-h"

    private fun lineBreak(): String = System.lineSeparator()
}
