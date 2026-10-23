import { useEffect, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "../ui/accordion";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "../ui/table";

export function TableTwin({ title, headers, rows }: {
	title: string;
	headers: string[];
	rows: { key: string; cells: ReactNode[] }[];
}) {
	const { t } = useTranslation();
	const [open, setOpen] = useState(() => window.matchMedia?.("(max-width: 639px)").matches ?? false);
	useEffect(() => {
		const media = window.matchMedia?.("(max-width: 639px)");
		if (!media) return;
		const update = () => setOpen(media.matches);
		media.addEventListener("change", update);
		return () => media.removeEventListener("change", update);
	}, []);
	return (
		<Accordion type="single" collapsible value={open ? "table" : ""} onValueChange={(value) => setOpen(value === "table")}>
			<AccordionItem value="table" className="border-0">
				<AccordionTrigger className="py-2 text-caption text-muted-foreground" headerClassName="rounded-none hover:bg-transparent data-[state=open]:bg-transparent">
					{t("benchmarks.tableTwin", { title })}
				</AccordionTrigger>
				<AccordionContent>
					<Table aria-label={title} className="table-fixed text-caption">
						<TableHeader><TableRow>{headers.map((header) => <TableHead key={header} className="whitespace-normal break-words">{header}</TableHead>)}</TableRow></TableHeader>
						<TableBody>{rows.map((row) => <TableRow key={row.key}>{row.cells.map((cell, index) => <TableCell key={`${row.key}-${index}`} className="whitespace-normal break-words">{cell}</TableCell>)}</TableRow>)}</TableBody>
					</Table>
				</AccordionContent>
			</AccordionItem>
		</Accordion>
	);
}
